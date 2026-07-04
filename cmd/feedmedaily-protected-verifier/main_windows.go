//go:build windows

package main

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/wailsapp/go-webview2/pkg/edge"
	"golang.org/x/sys/windows"
)

const (
	windowWidth  = 1240
	windowHeight = 920
	statusHeight = 64

	wmDestroy     = 0x0002
	wmSize        = 0x0005
	wmClose       = 0x0010
	wmAppAction   = 0x8001
	wsCaption     = 0x00C00000
	wsSysMenu     = 0x00080000
	wsThickFrame  = 0x00040000
	wsMinimizeBox = 0x00020000
	wsMaximizeBox = 0x00010000
	wsVisible     = 0x10000000
	wsChild       = 0x40000000
	wsExTopmost   = 0x00000008
	swShow        = 5
	colorWindow   = 5
	cwUseDefault  = ^uintptr(0x7fffffff)
	maxNavRetries = 3
	idcArrow      = 32512
	hwndTopmost   = ^uintptr(0)
	hwndNoTopmost = ^uintptr(1)
	swpNoMove     = 0x0002
	swpNoSize     = 0x0001
	swpShowWindow = 0x0040
)

var (
	user32               = windows.NewLazySystemDLL("user32.dll")
	kernel32             = windows.NewLazySystemDLL("kernel32.dll")
	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procGetMessageW      = user32.NewProc("GetMessageW")
	procLoadCursorW      = user32.NewProc("LoadCursorW")
	procPostMessageW     = user32.NewProc("PostMessageW")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")
	procSetForegroundWin = user32.NewProc("SetForegroundWindow")
	procSetWindowPos     = user32.NewProc("SetWindowPos")
	procSetWindowTextW   = user32.NewProc("SetWindowTextW")
	procShowWindow       = user32.NewProc("ShowWindow")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
)

type point struct {
	X int32
	Y int32
}

type msg struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type verifierApp struct {
	opts     cliOptions
	log      verifierLogger
	hwnd     uintptr
	status   uintptr
	chromium *edge.Chromium

	responseHandler *webResourceResponseReceivedHandler
	contentHandlers []*responseContentHandler

	mu                sync.Mutex
	actions           []func()
	remaining         []string
	currentFeedURL    string
	capturedFeeds     map[string]capturedFeed
	skippedFeeds      map[string]string
	approvalRefresh   map[string]bool
	navigationRetries map[string]int
	completionPosted  bool
	needsUserPosted   bool
}

var activeApp *verifierApp

func main() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	opts, err := parseOptions(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	app := newVerifierApp(opts)
	if err := app.run(); err != nil {
		app.log.Printf("startup failed: %s", err)
		_ = app.postTerminalResult("failed", false, err.Error())
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newVerifierApp(opts cliOptions) *verifierApp {
	return &verifierApp{
		opts:              opts,
		log:               newVerifierLogger(opts),
		remaining:         append([]string(nil), opts.FeedURLs...),
		capturedFeeds:     map[string]capturedFeed{},
		skippedFeeds:      map[string]string{},
		approvalRefresh:   map[string]bool{},
		navigationRetries: map[string]int{},
	}
}

func (a *verifierApp) run() error {
	activeApp = a
	a.log.Printf("started verification_id=%s host=%s feeds=%d", a.opts.VerificationID, a.opts.VerificationHost, len(a.opts.FeedURLs))
	if err := os.MkdirAll(a.opts.UserDataDir, 0o755); err != nil {
		return err
	}
	hwnd, err := a.createWindow()
	if err != nil {
		return err
	}
	a.hwnd = hwnd
	a.setStatus("FeedMeDaily is opening protected feeds in a persistent WebView2 profile. If Cloudflare asks for a human check, complete it here and leave the window open while the remaining feeds load.")

	chromium := edge.NewChromium()
	chromium.DataPath = a.opts.UserDataDir
	chromium.NavigationCompletedCallback = func(_ *edge.ICoreWebView2, args *edge.ICoreWebView2NavigationCompletedEventArgs) {
		a.onNavigationCompleted(uintptr(unsafe.Pointer(args)))
	}
	chromium.SetErrorCallback(func(err error) {
		a.enqueue(func() { a.failAndClose(err.Error()) })
	})
	a.chromium = chromium

	procShowWindow.Call(hwnd, swShow)
	procSetForegroundWin.Call(hwnd)
	procSetWindowPos.Call(hwnd, hwndTopmost, 0, 0, 0, 0, swpNoMove|swpNoSize|swpShowWindow)
	time.AfterFunc(1200*time.Millisecond, func() {
		a.enqueue(func() {
			procSetWindowPos.Call(hwnd, hwndNoTopmost, 0, 0, 0, 0, swpNoMove|swpNoSize|swpShowWindow)
		})
	})

	if !chromium.Embed(hwnd) {
		return fmt.Errorf("embed WebView2")
	}
	chromium.SetPadding(edge.Rect{Top: statusHeight})
	if err := a.registerResponseHandler(); err != nil {
		return err
	}
	time.AfterFunc(60*time.Second, func() {
		a.enqueue(func() { a.postNeedsUserIfNeeded("watchdog fired") })
	})
	a.navigateNextFeed()
	a.messageLoop()
	return nil
}

func (a *verifierApp) createWindow() (uintptr, error) {
	className, _ := windows.UTF16PtrFromString("FeedMeDailyProtectedVerifierWindow")
	title, _ := windows.UTF16PtrFromString("Protected Feed Verification")
	hinst, _, _ := procGetModuleHandleW.Call(0)
	cursor, _, _ := procLoadCursorW.Call(0, idcArrow)
	wc := wndClassEx{
		Size:       uint32(unsafe.Sizeof(wndClassEx{})),
		WndProc:    windows.NewCallback(wndProc),
		Instance:   hinst,
		Cursor:     cursor,
		Background: colorWindow + 1,
		ClassName:  className,
	}
	if atom, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); atom == 0 {
		return 0, fmt.Errorf("register verifier window class: %w", err)
	}
	style := uintptr(wsCaption | wsSysMenu | wsThickFrame | wsMinimizeBox | wsMaximizeBox | wsVisible)
	hwnd, _, err := procCreateWindowExW.Call(
		wsExTopmost,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		style,
		cwUseDefault,
		cwUseDefault,
		windowWidth,
		windowHeight,
		0,
		0,
		hinst,
		0,
	)
	if hwnd == 0 {
		return 0, fmt.Errorf("create verifier window: %w", err)
	}
	staticClass, _ := windows.UTF16PtrFromString("STATIC")
	statusText, _ := windows.UTF16PtrFromString("")
	a.status, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(staticClass)),
		uintptr(unsafe.Pointer(statusText)),
		wsChild|wsVisible,
		0,
		0,
		windowWidth,
		statusHeight,
		hwnd,
		0,
		hinst,
		0,
	)
	return hwnd, nil
}

func (a *verifierApp) messageLoop() {
	var m msg
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 {
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func (a *verifierApp) enqueue(fn func()) {
	a.mu.Lock()
	a.actions = append(a.actions, fn)
	a.mu.Unlock()
	if a.hwnd != 0 {
		procPostMessageW.Call(a.hwnd, wmAppAction, 0, 0)
	}
}

func (a *verifierApp) drainActions() {
	a.mu.Lock()
	actions := append([]func(){}, a.actions...)
	a.actions = nil
	a.mu.Unlock()
	for _, action := range actions {
		action()
	}
}

func (a *verifierApp) setStatus(text string) {
	if a.status == 0 {
		return
	}
	ptr, _ := windows.UTF16PtrFromString(text)
	procSetWindowTextW.Call(a.status, uintptr(unsafe.Pointer(ptr)))
}

func (a *verifierApp) navigateNextFeed() {
	if len(a.remaining) == 0 {
		a.completeAndClose()
		return
	}
	a.currentFeedURL = a.remaining[0]
	a.remaining = a.remaining[1:]
	a.setStatus(fmt.Sprintf("Opening protected feed %d/%d. If Cloudflare appears, complete the check and keep this window open.", len(a.capturedFeeds)+1, len(a.opts.FeedURLs)))
	a.log.Printf("navigate feed=%s", a.currentFeedURL)
	a.chromium.Navigate(a.currentFeedURL)
}

func (a *verifierApp) registerResponseHandler() error {
	webview := chromiumWebView(a.chromium)
	if webview == 0 {
		return fmt.Errorf("WebView2 core was not initialized")
	}
	webview2v2 := queryInterface(webview, "{9E8F0CF8-E670-4B5E-B2BC-73E061E3184C}")
	if webview2v2 == 0 {
		return fmt.Errorf("WebView2 runtime does not expose WebResourceResponseReceived")
	}
	a.responseHandler = newWebResourceResponseReceivedHandler(a)
	var token eventRegistrationToken
	hr, _, _ := ((*iCoreWebView2_2)(unsafe.Pointer(webview2v2))).vtbl.AddWebResourceResponseReceived.Call(
		webview2v2,
		uintptr(unsafe.Pointer(a.responseHandler)),
		uintptr(unsafe.Pointer(&token)),
	)
	if windows.Handle(hr) != windows.S_OK {
		return syscall.Errno(hr)
	}
	return nil
}

func (a *verifierApp) onNavigationCompleted(args uintptr) {
	success := int32(0)
	navArgs := (*navigationCompletedEventArgs)(unsafe.Pointer(args))
	hr, _, _ := navArgs.vtbl.GetIsSuccess.Call(args, uintptr(unsafe.Pointer(&success)))
	if windows.Handle(hr) != windows.S_OK {
		a.log.Printf("navigation status unavailable feed=%s error=0x%x", a.currentFeedURL, hr)
		return
	}
	if success != 0 {
		a.log.Printf("navigation completed feed=%s", a.currentFeedURL)
		if a.needsUserPosted {
			a.setStatus("Cloudflare approval received. FeedMeDaily is now collecting the remaining protected-feed XML documents.")
			a.refreshAfterApproval(a.currentFeedURL)
		} else {
			a.setStatus("Checking whether this protected feed now resolves to XML.")
		}
		return
	}
	a.log.Printf("navigation failed feed=%s", a.currentFeedURL)
	a.setStatus("The page has not fully loaded yet. If Cloudflare appears, complete the human verification and keep the window open.")
	a.retryNavigation(a.currentFeedURL)
}

func (a *verifierApp) refreshAfterApproval(feedURL string) {
	if strings.TrimSpace(feedURL) == "" {
		return
	}
	a.mu.Lock()
	if a.completionPosted || a.approvalRefresh[feedURL] {
		a.mu.Unlock()
		return
	}
	if _, ok := a.capturedFeeds[feedURL]; ok {
		a.mu.Unlock()
		return
	}
	a.approvalRefresh[feedURL] = true
	a.mu.Unlock()

	time.AfterFunc(900*time.Millisecond, func() {
		a.enqueue(func() {
			a.mu.Lock()
			_, captured := a.capturedFeeds[feedURL]
			shouldRefresh := !a.completionPosted && !captured && a.currentFeedURL == feedURL
			a.mu.Unlock()
			if !shouldRefresh {
				return
			}
			a.log.Printf("refresh after approval feed=%s", feedURL)
			a.chromium.Navigate(feedURL)
		})
	})
}

func (a *verifierApp) retryNavigation(feedURL string) {
	if strings.TrimSpace(feedURL) == "" {
		return
	}
	a.mu.Lock()
	if a.completionPosted {
		a.mu.Unlock()
		return
	}
	if _, ok := a.capturedFeeds[feedURL]; ok {
		a.mu.Unlock()
		return
	}
	a.navigationRetries[feedURL]++
	attempt := a.navigationRetries[feedURL]
	a.mu.Unlock()
	if attempt > maxNavRetries {
		a.log.Printf("navigation retry limit reached feed=%s attempts=%d", feedURL, attempt-1)
		a.enqueue(func() { a.skipCurrentFeed(feedURL, "navigation retry limit reached") })
		return
	}
	time.AfterFunc(time.Duration(attempt*4)*time.Second, func() {
		a.enqueue(func() {
			a.mu.Lock()
			_, captured := a.capturedFeeds[feedURL]
			shouldRetry := !a.completionPosted && !captured && a.currentFeedURL == feedURL
			a.mu.Unlock()
			if !shouldRetry {
				return
			}
			a.log.Printf("retry navigation feed=%s attempt=%d/%d", feedURL, attempt, maxNavRetries)
			a.chromium.Navigate(feedURL)
		})
	})
}

func (a *verifierApp) onResponseReceived(args uintptr) {
	a.mu.Lock()
	if a.completionPosted || a.currentFeedURL == "" {
		a.mu.Unlock()
		return
	}
	currentFeed := a.currentFeedURL
	if _, ok := a.capturedFeeds[currentFeed]; ok {
		a.mu.Unlock()
		return
	}
	a.mu.Unlock()

	eventArgs := (*webResourceResponseReceivedEventArgs)(unsafe.Pointer(args))
	var request *webResourceRequest
	hr, _, _ := eventArgs.vtbl.GetRequest.Call(args, uintptr(unsafe.Pointer(&request)))
	if windows.Handle(hr) != windows.S_OK || request == nil {
		return
	}
	requestURI := request.getURI()
	if !sameFeedURL(requestURI, currentFeed) {
		return
	}
	var response *webResourceResponseView
	hr, _, _ = eventArgs.vtbl.GetResponse.Call(args, uintptr(unsafe.Pointer(&response)))
	if windows.Handle(hr) != windows.S_OK || response == nil {
		a.log.Printf("response unavailable feed=%s error=0x%x", currentFeed, hr)
		return
	}
	contentType := response.header("Content-Type")
	handler := newResponseContentHandler(a, currentFeed, contentType)
	a.mu.Lock()
	a.contentHandlers = append(a.contentHandlers, handler)
	a.mu.Unlock()
	hr, _, _ = response.vtbl.GetContent.Call(uintptr(unsafe.Pointer(response)), uintptr(unsafe.Pointer(handler)))
	if windows.Handle(hr) != windows.S_OK {
		a.log.Printf("response content unavailable feed=%s error=0x%x", currentFeed, hr)
	}
}

func (a *verifierApp) onResponseBody(feedURL, contentType, body string) {
	a.log.Printf("response feed=%s content_type=%s bytes=%d", feedURL, contentType, len(body))
	if looksLikeXML(contentType, body) {
		a.mu.Lock()
		if _, ok := a.capturedFeeds[feedURL]; !ok {
			a.capturedFeeds[feedURL] = capturedFeed{FeedURL: feedURL, ContentType: contentType, FeedXML: body}
		}
		count := len(a.capturedFeeds)
		a.mu.Unlock()
		a.log.Printf("captured xml feed=%s captured=%d/%d", feedURL, count, len(a.opts.FeedURLs))
		a.enqueue(func() {
			a.setStatus(fmt.Sprintf("Captured %d/%d protected-feed XML documents.", count, len(a.opts.FeedURLs)))
			a.navigateNextFeed()
		})
		return
	}
	if looksLikeChallenge(contentType, body) {
		a.log.Printf("challenge detected feed=%s", feedURL)
		a.enqueue(func() {
			a.setStatus("Cloudflare still needs a human check in this window. Complete it once and FeedMeDaily will keep trying the remaining protected feeds automatically.")
			a.postNeedsUserIfNeeded("")
		})
		return
	}
	a.enqueue(func() {
		a.skipCurrentFeed(feedURL, fmt.Sprintf("non-XML response content_type=%s", contentType))
	})
}

func (a *verifierApp) skipCurrentFeed(feedURL, reason string) {
	a.mu.Lock()
	if a.completionPosted || feedURL == "" || a.currentFeedURL != feedURL {
		a.mu.Unlock()
		return
	}
	if _, captured := a.capturedFeeds[feedURL]; captured {
		a.mu.Unlock()
		return
	}
	a.skippedFeeds[feedURL] = reason
	skippedCount := len(a.skippedFeeds)
	total := len(a.opts.FeedURLs)
	a.mu.Unlock()
	a.log.Printf("skip feed=%s reason=%s", feedURL, reason)
	a.setStatus(fmt.Sprintf("Skipped %d/%d protected feeds that did not return XML; continuing with the next feed.", skippedCount, total))
	a.navigateNextFeed()
}

func (a *verifierApp) postNeedsUserIfNeeded(reason string) {
	a.mu.Lock()
	if a.completionPosted || a.needsUserPosted {
		a.mu.Unlock()
		return
	}
	a.needsUserPosted = true
	feedURL := a.currentFeedURL
	a.mu.Unlock()
	if reason != "" {
		a.log.Printf("needs_user %s feed=%s", reason, feedURL)
	}
	payload := callbackPayload{
		VerificationID:   a.opts.VerificationID,
		VerificationHost: a.opts.VerificationHost,
		FeedURL:          feedURL,
		Status:           "needs_user",
		ContentType:      "application/xml",
		SessionVerified:  false,
		CapturedFeeds:    []capturedFeed{},
	}
	a.log.Printf("post needs_user feed=%s", feedURL)
	statusCode, status, err := postPayload(a.opts.CallbackURL, payload)
	if err != nil {
		a.log.Printf("callback failed: %s", err)
		return
	}
	a.log.Printf("callback status=%d reason=%s", statusCode, status)
}

func (a *verifierApp) completeAndClose() {
	a.mu.Lock()
	capturedCount := len(a.capturedFeeds)
	a.mu.Unlock()
	if capturedCount == 0 {
		a.failAndClose("the protected-feed verifier did not capture any feed XML")
		return
	}
	a.log.Printf("complete captured=%d", capturedCount)
	_ = a.postTerminalResult("success", true, "")
	procDestroyWindow.Call(a.hwnd)
}

func (a *verifierApp) failAndClose(message string) {
	a.log.Printf("failed error=%s", message)
	_ = a.postTerminalResult("failed", len(a.capturedFeeds) > 0, message)
	procDestroyWindow.Call(a.hwnd)
}

func (a *verifierApp) onClose() {
	a.mu.Lock()
	alreadyDone := a.completionPosted
	captured := len(a.capturedFeeds) > 0
	a.mu.Unlock()
	if !alreadyDone {
		a.log.Printf("window closed before completion")
		_ = a.postTerminalResult("aborted", captured, "the protected-feed verification window was closed before all feed XML was captured")
	}
	procDestroyWindow.Call(a.hwnd)
}

func (a *verifierApp) postTerminalResult(status string, sessionVerified bool, errorMessage string) error {
	a.mu.Lock()
	if a.completionPosted {
		a.mu.Unlock()
		return nil
	}
	a.completionPosted = true
	feedURL := a.currentFeedURL
	captured := orderedCapturedFeeds(a.capturedFeeds)
	a.mu.Unlock()
	payload := callbackPayload{
		VerificationID:   a.opts.VerificationID,
		VerificationHost: a.opts.VerificationHost,
		FeedURL:          feedURL,
		Status:           status,
		ContentType:      "application/xml",
		Error:            errorMessage,
		SessionVerified:  sessionVerified,
		CapturedFeeds:    captured,
	}
	a.log.Printf("post terminal status=%s session_verified=%t captured=%d error=%s", status, sessionVerified, len(captured), errorMessage)
	statusCode, responseStatus, err := postPayload(a.opts.CallbackURL, payload)
	if err != nil {
		a.log.Printf("callback failed: %s", err)
		return err
	}
	a.log.Printf("callback status=%d reason=%s", statusCode, responseStatus)
	return nil
}

func wndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	if activeApp != nil {
		switch msg {
		case wmSize:
			if activeApp.chromium != nil {
				activeApp.chromium.Resize()
			}
			return 0
		case wmAppAction:
			activeApp.drainActions()
			return 0
		case wmClose:
			activeApp.onClose()
			return 0
		case wmDestroy:
			procPostQuitMessage.Call(0)
			return 0
		}
	}
	ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return ret
}

func chromiumWebView(chromium *edge.Chromium) uintptr {
	value := reflect.ValueOf(chromium).Elem().FieldByName("webview")
	return *(*uintptr)(unsafe.Pointer(value.UnsafeAddr()))
}

func queryInterface(this uintptr, guid string) uintptr {
	unknown := (*iUnknown)(unsafe.Pointer(this))
	iid := edgeGUID(guid)
	var result uintptr
	hr, _, _ := unknown.vtbl.QueryInterface.Call(this, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&result)))
	if windows.Handle(hr) != windows.S_OK {
		return 0
	}
	return result
}

type eventRegistrationToken struct {
	value int64
}

type iUnknownVtbl struct {
	QueryInterface edge.ComProc
	AddRef         edge.ComProc
	Release        edge.ComProc
}

type iUnknown struct {
	vtbl *iUnknownVtbl
}

type iCoreWebView2_2Vtbl struct {
	iUnknownVtbl
	GetSettings                            edge.ComProc
	GetSource                              edge.ComProc
	Navigate                               edge.ComProc
	NavigateToString                       edge.ComProc
	AddNavigationStarting                  edge.ComProc
	RemoveNavigationStarting               edge.ComProc
	AddContentLoading                      edge.ComProc
	RemoveContentLoading                   edge.ComProc
	AddSourceChanged                       edge.ComProc
	RemoveSourceChanged                    edge.ComProc
	AddHistoryChanged                      edge.ComProc
	RemoveHistoryChanged                   edge.ComProc
	AddNavigationCompleted                 edge.ComProc
	RemoveNavigationCompleted              edge.ComProc
	AddFrameNavigationStarting             edge.ComProc
	RemoveFrameNavigationStarting          edge.ComProc
	AddFrameNavigationCompleted            edge.ComProc
	RemoveFrameNavigationCompleted         edge.ComProc
	AddScriptDialogOpening                 edge.ComProc
	RemoveScriptDialogOpening              edge.ComProc
	AddPermissionRequested                 edge.ComProc
	RemovePermissionRequested              edge.ComProc
	AddProcessFailed                       edge.ComProc
	RemoveProcessFailed                    edge.ComProc
	AddScriptToExecuteOnDocumentCreated    edge.ComProc
	RemoveScriptToExecuteOnDocumentCreated edge.ComProc
	ExecuteScript                          edge.ComProc
	CapturePreview                         edge.ComProc
	Reload                                 edge.ComProc
	PostWebMessageAsJSON                   edge.ComProc
	PostWebMessageAsString                 edge.ComProc
	AddWebMessageReceived                  edge.ComProc
	RemoveWebMessageReceived               edge.ComProc
	CallDevToolsProtocolMethod             edge.ComProc
	GetBrowserProcessID                    edge.ComProc
	GetCanGoBack                           edge.ComProc
	GetCanGoForward                        edge.ComProc
	GoBack                                 edge.ComProc
	GoForward                              edge.ComProc
	GetDevToolsProtocolEventReceiver       edge.ComProc
	Stop                                   edge.ComProc
	AddNewWindowRequested                  edge.ComProc
	RemoveNewWindowRequested               edge.ComProc
	AddDocumentTitleChanged                edge.ComProc
	RemoveDocumentTitleChanged             edge.ComProc
	GetDocumentTitle                       edge.ComProc
	AddHostObjectToScript                  edge.ComProc
	RemoveHostObjectFromScript             edge.ComProc
	OpenDevToolsWindow                     edge.ComProc
	AddContainsFullScreenElementChanged    edge.ComProc
	RemoveContainsFullScreenElementChanged edge.ComProc
	GetContainsFullScreenElement           edge.ComProc
	AddWebResourceRequested                edge.ComProc
	RemoveWebResourceRequested             edge.ComProc
	AddWebResourceRequestedFilter          edge.ComProc
	RemoveWebResourceRequestedFilter       edge.ComProc
	AddWindowCloseRequested                edge.ComProc
	RemoveWindowCloseRequested             edge.ComProc
	AddWebResourceResponseReceived         edge.ComProc
	RemoveWebResourceResponseReceived      edge.ComProc
}

type iCoreWebView2_2 struct {
	vtbl *iCoreWebView2_2Vtbl
}

type navigationCompletedEventArgsVtbl struct {
	iUnknownVtbl
	GetIsSuccess      edge.ComProc
	GetWebErrorStatus edge.ComProc
	GetNavigationID   edge.ComProc
}

type navigationCompletedEventArgs struct {
	vtbl *navigationCompletedEventArgsVtbl
}

type webResourceResponseReceivedEventArgsVtbl struct {
	iUnknownVtbl
	GetRequest  edge.ComProc
	GetResponse edge.ComProc
}

type webResourceResponseReceivedEventArgs struct {
	vtbl *webResourceResponseReceivedEventArgsVtbl
}

type webResourceRequestVtbl struct {
	iUnknownVtbl
	GetURI     edge.ComProc
	PutURI     edge.ComProc
	GetMethod  edge.ComProc
	PutMethod  edge.ComProc
	GetContent edge.ComProc
	PutContent edge.ComProc
	GetHeaders edge.ComProc
}

type webResourceRequest struct {
	vtbl *webResourceRequestVtbl
}

func (r *webResourceRequest) getURI() string {
	var value *uint16
	hr, _, _ := r.vtbl.GetURI.Call(uintptr(unsafe.Pointer(r)), uintptr(unsafe.Pointer(&value)))
	if windows.Handle(hr) != windows.S_OK || value == nil {
		return ""
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(value))
	return windows.UTF16PtrToString(value)
}

type webResourceResponseViewVtbl struct {
	iUnknownVtbl
	GetHeaders      edge.ComProc
	GetStatusCode   edge.ComProc
	GetReasonPhrase edge.ComProc
	GetContent      edge.ComProc
}

type webResourceResponseView struct {
	vtbl *webResourceResponseViewVtbl
}

func (r *webResourceResponseView) header(name string) string {
	var headers *httpResponseHeaders
	hr, _, _ := r.vtbl.GetHeaders.Call(uintptr(unsafe.Pointer(r)), uintptr(unsafe.Pointer(&headers)))
	if windows.Handle(hr) != windows.S_OK || headers == nil {
		return ""
	}
	return headers.getHeader(name)
}

type httpResponseHeadersVtbl struct {
	iUnknownVtbl
	AppendHeader edge.ComProc
	Contains     edge.ComProc
	GetHeader    edge.ComProc
	GetHeaders   edge.ComProc
	GetIterator  edge.ComProc
}

type httpResponseHeaders struct {
	vtbl *httpResponseHeadersVtbl
}

func (h *httpResponseHeaders) getHeader(name string) string {
	headerName, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return ""
	}
	var value *uint16
	hr, _, _ := h.vtbl.GetHeader.Call(uintptr(unsafe.Pointer(h)), uintptr(unsafe.Pointer(headerName)), uintptr(unsafe.Pointer(&value)))
	if windows.Handle(hr) != windows.S_OK || value == nil {
		return ""
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(value))
	return windows.UTF16PtrToString(value)
}

type iStreamVtbl struct {
	iUnknownVtbl
	Read  edge.ComProc
	Write edge.ComProc
}

type iStream struct {
	vtbl *iStreamVtbl
}

func (s *iStream) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	var n int
	hr, _, err := s.vtbl.Read.Call(uintptr(unsafe.Pointer(s)), uintptr(unsafe.Pointer(&p[0])), uintptr(len(p)), uintptr(unsafe.Pointer(&n)))
	if err != windows.ERROR_SUCCESS {
		return 0, err
	}
	switch windows.Handle(hr) {
	case windows.S_OK:
		return n, nil
	case windows.S_FALSE:
		return n, io.EOF
	default:
		return 0, syscall.Errno(hr)
	}
}

type webResourceResponseReceivedHandler struct {
	vtbl *responseReceivedHandlerVtbl
	ref  atomic.Uint32
	app  *verifierApp
}

type responseReceivedHandlerVtbl struct {
	QueryInterface edge.ComProc
	AddRef         edge.ComProc
	Release        edge.ComProc
	Invoke         edge.ComProc
}

var responseReceivedHandlerVTable = responseReceivedHandlerVtbl{
	QueryInterface: edge.NewComProc(responseHandlerQueryInterface),
	AddRef:         edge.NewComProc(responseHandlerAddRef),
	Release:        edge.NewComProc(responseHandlerRelease),
	Invoke:         edge.NewComProc(responseHandlerInvoke),
}

func newWebResourceResponseReceivedHandler(app *verifierApp) *webResourceResponseReceivedHandler {
	handler := &webResourceResponseReceivedHandler{vtbl: &responseReceivedHandlerVTable, app: app}
	handler.ref.Store(1)
	return handler
}

func responseHandlerQueryInterface(this, _ uintptr, object uintptr) uintptr {
	if object != 0 {
		*(*uintptr)(unsafe.Pointer(object)) = this
		(*webResourceResponseReceivedHandler)(unsafe.Pointer(this)).ref.Add(1)
	}
	return 0
}

func responseHandlerAddRef(this uintptr) uintptr {
	return uintptr((*webResourceResponseReceivedHandler)(unsafe.Pointer(this)).ref.Add(1))
}

func responseHandlerRelease(this uintptr) uintptr {
	return uintptr((*webResourceResponseReceivedHandler)(unsafe.Pointer(this)).ref.Add(^uint32(0)))
}

func responseHandlerInvoke(this, _ uintptr, args uintptr) uintptr {
	(*webResourceResponseReceivedHandler)(unsafe.Pointer(this)).app.onResponseReceived(args)
	return 0
}

type responseContentHandler struct {
	vtbl        *contentHandlerVtbl
	ref         atomic.Uint32
	app         *verifierApp
	feedURL     string
	contentType string
}

type contentHandlerVtbl struct {
	QueryInterface edge.ComProc
	AddRef         edge.ComProc
	Release        edge.ComProc
	Invoke         edge.ComProc
}

var contentHandlerVTable = contentHandlerVtbl{
	QueryInterface: edge.NewComProc(contentHandlerQueryInterface),
	AddRef:         edge.NewComProc(contentHandlerAddRef),
	Release:        edge.NewComProc(contentHandlerRelease),
	Invoke:         edge.NewComProc(contentHandlerInvoke),
}

func newResponseContentHandler(app *verifierApp, feedURL string, contentType string) *responseContentHandler {
	handler := &responseContentHandler{vtbl: &contentHandlerVTable, app: app, feedURL: feedURL, contentType: contentType}
	handler.ref.Store(1)
	return handler
}

func contentHandlerQueryInterface(this, _ uintptr, object uintptr) uintptr {
	if object != 0 {
		*(*uintptr)(unsafe.Pointer(object)) = this
		(*responseContentHandler)(unsafe.Pointer(this)).ref.Add(1)
	}
	return 0
}

func contentHandlerAddRef(this uintptr) uintptr {
	return uintptr((*responseContentHandler)(unsafe.Pointer(this)).ref.Add(1))
}

func contentHandlerRelease(this uintptr) uintptr {
	return uintptr((*responseContentHandler)(unsafe.Pointer(this)).ref.Add(^uint32(0)))
}

func contentHandlerInvoke(this, errorCode uintptr, streamPtr uintptr) uintptr {
	handler := (*responseContentHandler)(unsafe.Pointer(this))
	if int32(errorCode) < 0 || streamPtr == 0 {
		handler.app.log.Printf("response body unavailable feed=%s error=0x%x", handler.feedURL, errorCode)
		handler.app.enqueue(func() {
			handler.app.skipCurrentFeed(handler.feedURL, fmt.Sprintf("response body unavailable error=0x%x", errorCode))
		})
		return errorCode
	}
	stream := (*iStream)(unsafe.Pointer(streamPtr))
	body, err := io.ReadAll(stream)
	if err != nil {
		handler.app.log.Printf("response body read failed feed=%s error=%s", handler.feedURL, err)
		return 0
	}
	handler.app.onResponseBody(handler.feedURL, handler.contentType, string(body))
	return 0
}

type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

func edgeGUID(value string) *guid {
	parsed, err := windows.GUIDFromString(value)
	if err != nil {
		return nil
	}
	return (*guid)(unsafe.Pointer(&parsed))
}

func sameFeedURL(actual, expected string) bool {
	actual = strings.TrimSpace(actual)
	expected = strings.TrimSpace(expected)
	if strings.EqualFold(actual, expected) {
		return true
	}
	a, errA := url.Parse(actual)
	e, errE := url.Parse(expected)
	if errA != nil || errE != nil || a.Host == "" || e.Host == "" {
		return false
	}
	a.Fragment = ""
	e.Fragment = ""
	a.Scheme = strings.ToLower(a.Scheme)
	e.Scheme = strings.ToLower(e.Scheme)
	a.Host = strings.ToLower(a.Host)
	e.Host = strings.ToLower(e.Host)
	a.Path = strings.TrimRight(a.EscapedPath(), "/")
	e.Path = strings.TrimRight(e.EscapedPath(), "/")
	if a.Path == "" {
		a.Path = "/"
	}
	if e.Path == "" {
		e.Path = "/"
	}
	return a.Scheme == e.Scheme &&
		a.Host == e.Host &&
		a.Path == e.Path &&
		a.RawQuery == e.RawQuery
}

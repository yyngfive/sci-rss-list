package main

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"sci-rss-list/internal/catalog"
)

type fetchResult struct {
	status string
	detail string
}

const feedcheckUserAgent = "FeedFetcher-Google; (+http://www.google.com/feedfetcher.html)"

type capturedFeed struct {
	FeedURL     string `json:"feed_url"`
	ContentType string `json:"content_type"`
	FeedXML     string `json:"feed_xml"`
}

type callbackPayload struct {
	VerificationID   string         `json:"verification_id"`
	VerificationHost string         `json:"verification_host"`
	FeedURL          string         `json:"feed_url"`
	Status           string         `json:"status"`
	ContentType      string         `json:"content_type"`
	FeedXML          string         `json:"feed_xml"`
	Error            string         `json:"error"`
	SessionVerified  bool           `json:"session_verified"`
	CapturedFeeds    []capturedFeed `json:"captured_feeds"`
}

type manualResult struct {
	captured map[string]capturedFeed
	status   string
	err      string
}

func main() {
	requestTimeout := flag.Duration("request-timeout", 25*time.Second, "ordinary HTTP request timeout")
	manualTimeout := flag.Duration("manual-timeout", 10*time.Minute, "manual WebView2 verification timeout per protected host")
	force := flag.Bool("force", false, "re-verify entries whose JSON status is already verified")
	flag.Parse()

	root, err := os.Getwd()
	if err != nil {
		die(err)
	}
	feeds, raw, err := catalog.Load(filepath.Join(root, "data", "feeds.json"))
	if err != nil {
		die(err)
	}
	dataPath := filepath.Join(root, "data", "feeds.json")

	errs := catalog.ValidateShape(feeds, raw)
	if len(errs) == 0 {
		changed := false
		errs = append(errs, validateFeeds(root, feeds, *requestTimeout, *manualTimeout, *force, &changed)...)
		if changed {
			if err := catalog.Save(dataPath, feeds); err != nil {
				errs = append(errs, err.Error())
			} else {
				fmt.Println("updated data/feeds.json with verified statuses")
			}
		}
		if err := catalog.WritePublisherMarkdown(filepath.Join(root, "publishers"), feeds); err != nil {
			errs = append(errs, err.Error())
		}
		if err := catalog.WriteReadmePublisherIndex(filepath.Join(root, "README.md"), feeds); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		for _, err := range errs {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
	fmt.Printf("ok: %d entries, %d publishers\n", len(feeds), catalog.PublisherCount(feeds))
}

func validateFeeds(root string, feeds []catalog.Feed, requestTimeout, manualTimeout time.Duration, force bool, changed *bool) []string {
	var errs []string
	protected := map[string][]catalog.Feed{}
	pending := pendingFeedIndexes(feeds, force)
	fmt.Printf("catalog feeds: %d; json verified: %d; validating: %d\n", len(feeds), len(feeds)-len(pending), len(pending))
	if len(pending) == 0 {
		return nil
	}
	for i, idx := range pending {
		f := feeds[idx]
		fmt.Printf("http %d/%d: [%s] %s\n  %s\n", i+1, len(pending), f.Publisher, f.Journal, f.URL)
		actual := fetchStatus(f.URL, requestTimeout)
		switch f.Status {
		case "verified":
			if actual.status != "verified" {
				if needsManualVerification(f.Status, actual, force) {
					if err := queueProtectedFeed(protected, feeds[idx]); err != nil {
						errs = append(errs, fmt.Sprintf("%s: bad url %s", f.Journal, f.URL))
					}
					continue
				}
				errs = append(errs, fmt.Sprintf("%s: expected verified, got %s (%s)", f.Journal, actual.detail, f.URL))
			} else {
				fmt.Println("  ok: feed XML")
			}
		case "protected":
			if actual.status == "verified" {
				markVerified(feeds, idx, changed)
				fmt.Println("  ok: feed XML without manual verification")
				continue
			}
			if !needsManualVerification(f.Status, actual, force) {
				errs = append(errs, fmt.Sprintf("%s: expected protected, got %s (%s)", f.Journal, actual.detail, f.URL))
				continue
			}
			if err := queueProtectedFeed(protected, feeds[idx]); err != nil {
				errs = append(errs, fmt.Sprintf("%s: bad url %s", f.Journal, f.URL))
				continue
			}
		case "source_documented":
			switch {
			case actual.status == "verified":
				markVerified(feeds, idx, changed)
				fmt.Println("  ok: source_documented feed XML")
			case needsManualVerification(f.Status, actual, force):
				if err := queueProtectedFeed(protected, feeds[idx]); err != nil {
					errs = append(errs, fmt.Sprintf("%s: bad url %s", f.Journal, f.URL))
					continue
				}
			case actual.status == "bad_url" || strings.HasPrefix(actual.status, "bad_http_"):
				errs = append(errs, fmt.Sprintf("%s: source_documented got %s (%s)", f.Journal, actual.detail, f.URL))
			default:
				fmt.Printf("  ok: source_documented (%s)\n", actual.status)
			}
		}
	}
	if len(protected) == 0 {
		return errs
	}
	if runtime.GOOS != "windows" {
		return append(errs, "protected feeds require the Windows WebView2 verifier")
	}

	hosts := make([]string, 0, len(protected))
	for host := range protected {
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)
	for i, host := range hosts {
		fmt.Printf("manual %d/%d: %s (%d feeds)\n", i+1, len(hosts), host, len(protected[host]))
		result, err := runManualVerifier(root, host, protected[host], manualTimeout)
		recordManualCaptures(feeds, result, changed)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: manual verifier failed: %v", host, err))
			continue
		}
		if result.status != "success" {
			fmt.Printf("  manual verifier ended with %s %s\n", result.status, result.err)
		}
		for _, f := range protected[host] {
			captured, ok := result.captured[f.URL]
			if !ok {
				if requiresManualCapture(f) {
					errs = append(errs, fmt.Sprintf("%s: manual verifier did not capture XML (%s)", f.Journal, f.URL))
				} else {
					fmt.Printf("  ok: source_documented not captured for [%s] %s\n", f.Publisher, f.Journal)
				}
				continue
			}
			if !isFeedXML([]byte(captured.FeedXML), captured.ContentType) {
				errs = append(errs, fmt.Sprintf("%s: manual verifier captured non-feed XML (%s)", f.Journal, f.URL))
			}
		}
	}
	return errs
}

func needsManualVerification(catalogStatus string, actual fetchResult, force bool) bool {
	return actual.status == "protected" && (catalogStatus == "protected" || catalogStatus == "source_documented" || (force && catalogStatus == "verified"))
}

func requiresManualCapture(f catalog.Feed) bool {
	return f.Status == "protected" || f.Status == "verified"
}

func queueProtectedFeed(protected map[string][]catalog.Feed, f catalog.Feed) error {
	host, err := urlHost(f.URL)
	if err != nil {
		return err
	}
	protected[host] = append(protected[host], f)
	fmt.Printf("  protected: queued for WebView2 verification on %s\n", host)
	return nil
}

func pendingFeedIndexes(feeds []catalog.Feed, force bool) []int {
	if force {
		pending := make([]int, len(feeds))
		for i := range feeds {
			pending[i] = i
		}
		return pending
	}
	pending := make([]int, 0, len(feeds))
	for i, f := range feeds {
		if f.Status != "verified" {
			pending = append(pending, i)
		}
	}
	return pending
}

func recordManualCaptures(feeds []catalog.Feed, result manualResult, changed *bool) {
	for i := range feeds {
		captured, ok := result.captured[feeds[i].URL]
		if !ok || !isFeedXML([]byte(captured.FeedXML), captured.ContentType) {
			continue
		}
		markVerified(feeds, i, changed)
		fmt.Printf("  ok: captured XML for [%s] %s\n", feeds[i].Publisher, feeds[i].Journal)
	}
}

func markVerified(feeds []catalog.Feed, i int, changed *bool) {
	if feeds[i].Status == "verified" {
		return
	}
	feeds[i].Status = "verified"
	if strings.TrimSpace(feeds[i].Notes) == "Generic validator receives a protected or challenge response." {
		feeds[i].Notes = ""
	}
	*changed = true
}

func fetchStatus(rawurl string, timeout time.Duration) fetchResult {
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequest(http.MethodGet, rawurl, nil)
	if err != nil {
		return fetchResult{"bad_url", err.Error()}
	}
	req.Header.Set("User-Agent", feedcheckUserAgent)
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/rdf+xml, application/xml, text/xml;q=0.9, */*;q=0.1")
	res, err := client.Do(req)
	if err != nil {
		return fetchResult{"unreachable", "unreachable: " + err.Error()}
	}
	defer res.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(res.Body, 128*1024))
	headers := strings.ToLower(fmt.Sprint(res.Header) + " " + res.Status)
	text := strings.ToLower(string(body))
	if isProtected(res.StatusCode, headers, text) {
		return fetchResult{"protected", "protected"}
	}
	if res.StatusCode >= 400 {
		return fetchResult{fmt.Sprintf("bad_http_%d", res.StatusCode), fmt.Sprintf("bad_http_%d", res.StatusCode)}
	}
	if isFeedXML(body, res.Header.Get("Content-Type")) {
		return fetchResult{"verified", "verified"}
	}
	return fetchResult{"source_documented", "source_documented"}
}

func runManualVerifier(root, host string, feeds []catalog.Feed, timeout time.Duration) (manualResult, error) {
	feedURLs := make([]string, 0, len(feeds))
	for _, f := range feeds {
		feedURLs = append(feedURLs, f.URL)
	}
	fmt.Printf("  opening WebView2 window for %s; complete any human check there\n", host)
	result := manualResult{captured: map[string]capturedFeed{}, status: "timeout"}
	callbackURL, stop, done := startCallbackServer(&result)
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	args := []string{
		"run", ".\\cmd\\feedmedaily-protected-verifier",
		"--verification-id", "sci-rss-list-" + catalog.Slugify(host),
		"--verification-host", host,
		"--callback-url", callbackURL,
		"--user-data-dir", filepath.Join(root, ".feedcheck-webview2", catalog.Slugify(host)),
		"--logs-dir", filepath.Join(root, ".feedcheck-webview2", "logs"),
	}
	for _, feedURL := range feedURLs {
		args = append(args, "--feed-url", feedURL)
	}
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return result, err
	}
	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()

	select {
	case <-done:
	case <-ctx.Done():
	case <-waitErr:
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	select {
	case err := <-waitErr:
		if err != nil {
			return result, err
		}
	default:
	}
	return result, nil
}

func startCallbackServer(result *manualResult) (string, func(), <-chan struct{}) {
	done := make(chan struct{})
	var once sync.Once
	var mu sync.Mutex
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var payload callbackPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		if payload.Status != "" {
			result.status = payload.Status
		}
		if payload.Error != "" {
			result.err = payload.Error
		}
		for _, item := range payload.CapturedFeeds {
			result.captured[item.FeedURL] = item
		}
		if payload.FeedURL != "" && payload.FeedXML != "" {
			result.captured[payload.FeedURL] = capturedFeed{FeedURL: payload.FeedURL, ContentType: payload.ContentType, FeedXML: payload.FeedXML}
		}
		terminal := payload.Status == "success" || payload.Status == "failed" || payload.Status == "aborted"
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
		if terminal {
			once.Do(func() { close(done) })
		}
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		die(err)
	}
	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(ln) }()
	stop := func() {
		_ = server.Close()
		once.Do(func() { close(done) })
	}
	return "http://" + ln.Addr().String(), stop, done
}

func isProtected(code int, headers, body string) bool {
	haystack := headers + " " + body
	hints := []string{
		"cf-mitigated", "cloudflare", "__cf_bm", "just a moment", "enable javascript",
		"verify you are human", "captcha", "unusual traffic", "access denied",
		"request blocked", "akamai", "perimeterx",
	}
	for _, hint := range hints {
		if strings.Contains(haystack, hint) {
			return true
		}
	}
	return code == http.StatusUnauthorized || code == http.StatusForbidden || code == http.StatusTooManyRequests
}

func isFeedXML(body []byte, contentType string) bool {
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "rss") || strings.Contains(ct, "atom") || strings.Contains(ct, "rdf") {
		return true
	}
	root, ok := xmlRoot(body)
	return ok && (root == "rss" || root == "feed" || root == "RDF")
}

func xmlRoot(body []byte) (string, bool) {
	dec := xml.NewDecoder(bytes.NewReader(body))
	for {
		tok, err := dec.Token()
		if err != nil {
			return "", false
		}
		if start, ok := tok.(xml.StartElement); ok {
			return start.Name.Local, true
		}
	}
}

func urlHost(rawurl string) (string, error) {
	u, err := url.Parse(rawurl)
	if err != nil {
		return "", err
	}
	if u.Host == "" {
		return "", fmt.Errorf("missing host")
	}
	return strings.ToLower(u.Host), nil
}

func die(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

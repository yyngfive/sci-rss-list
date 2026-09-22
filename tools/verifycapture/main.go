// Command verifycapture runs the feedmedaily-protected-verifier WebView2 UI for
// one protected host and records captured feed XML the way feedcheck does.
//
// It exists for hosts whose bot challenge feedcheck's ordinary HTTP check does
// not classify as protected (for example Radware or Incapsula interstitials),
// so those feeds never reach feedcheck's WebView2 queue and cannot be upgraded
// through a normal feedcheck run. Verification still happens inside the same
// WebView2 verifier; this command only supplies the callback receiver and the
// catalog close-out that feedcheck performs after a successful capture.
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
	"strings"
	"sync"
	"time"

	"sci-rss-list/internal/catalog"
)

type capturedFeed struct {
	FeedURL     string `json:"feed_url"`
	ContentType string `json:"content_type"`
	FeedXML     string `json:"feed_xml"`
}

type callbackPayload struct {
	Status        string         `json:"status"`
	Error         string         `json:"error"`
	FeedURL       string         `json:"feed_url"`
	ContentType   string         `json:"content_type"`
	FeedXML       string         `json:"feed_xml"`
	CapturedFeeds []capturedFeed `json:"captured_feeds"`
}

type captureState struct {
	mu       sync.Mutex
	captured map[string]capturedFeed
	status   string
	err      string
}

func main() {
	host := flag.String("host", "", "verification host, for example iopscience.iop.org")
	timeout := flag.Duration("timeout", 5*time.Minute, "WebView2 verification timeout")
	dryRun := flag.Bool("dry-run", false, "print selected feeds and exit")
	var feedURLs repeatedStrings
	flag.Var(&feedURLs, "feed", "feed URL to capture; repeatable; default is every non-verified entry of the host")
	flag.Parse()

	if strings.TrimSpace(*host) == "" {
		die(fmt.Errorf("-host is required"))
	}
	root, err := os.Getwd()
	if err != nil {
		die(err)
	}
	dataPath := filepath.Join(root, "data", "feeds.json")
	feeds, raw, err := catalog.Load(dataPath)
	if err != nil {
		die(err)
	}
	if errs := catalog.ValidateShape(feeds, raw); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, e)
		}
		os.Exit(1)
	}

	targets := selectFeeds(feeds, *host, feedURLs)
	if len(targets) == 0 {
		die(fmt.Errorf("no non-verified entries match host %s", *host))
	}
	fmt.Printf("host %s: %d feeds selected\n", *host, len(targets))
	for _, i := range targets {
		fmt.Printf("  [%s] %s\n    %s\n", feeds[i].Publisher, feeds[i].Journal, feeds[i].URL)
	}
	if *dryRun {
		return
	}

	slug := catalog.Slugify(*host)
	state := &captureState{captured: map[string]capturedFeed{}, status: "timeout"}
	callbackURL, stop, done := startCallbackServer(state)
	defer stop()

	args := []string{
		"run", ".\\cmd\\feedmedaily-protected-verifier",
		"--verification-id", "sci-rss-list-" + slug,
		"--verification-host", *host,
		"--callback-url", callbackURL,
		"--user-data-dir", filepath.Join(root, ".feedcheck-webview2", slug),
		"--logs-dir", filepath.Join(root, ".feedcheck-webview2", "logs"),
	}
	for _, i := range targets {
		args = append(args, "--feed-url", feeds[i].URL)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		die(err)
	}
	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()
	select {
	case <-done:
	case <-ctx.Done():
	case <-waitErr:
	}
	if ctx.Err() != nil {
		fmt.Println("verifier timed out; keeping uncaptured feeds unchanged")
	} else {
		select {
		case err := <-waitErr:
			if err != nil {
				fmt.Printf("verifier exited with %v\n", err)
			}
		default:
		}
	}

	state.mu.Lock()
	captured := state.captured
	status, reason := state.status, state.err
	state.mu.Unlock()
	fmt.Printf("verifier result: %s %s; captured %d feeds\n", status, reason, len(captured))

	changed := false
	var errs []string
	for _, i := range targets {
		f := feeds[i]
		item, ok := captured[f.URL]
		if !ok {
			if f.Status == "protected" || f.Status == "verified" {
				errs = append(errs, fmt.Sprintf("%s: verifier did not capture XML (%s)", f.Journal, f.URL))
			} else {
				fmt.Printf("ok: source_documented not captured for [%s] %s\n", f.Publisher, f.Journal)
			}
			continue
		}
		if !isFeedXML([]byte(item.FeedXML), item.ContentType) {
			errs = append(errs, fmt.Sprintf("%s: verifier captured non-feed XML (%s)", f.Journal, f.URL))
			continue
		}
		if feeds[i].Status != "verified" {
			feeds[i].Status = "verified"
			if strings.TrimSpace(feeds[i].Notes) == "Generic validator receives a protected or challenge response." {
				feeds[i].Notes = ""
			}
			changed = true
		}
		fmt.Printf("ok: captured XML for [%s] %s\n", f.Publisher, f.Journal)
	}

	if changed {
		if err := catalog.Save(dataPath, feeds); err != nil {
			errs = append(errs, err.Error())
		} else {
			fmt.Println("updated data/feeds.json with verified statuses")
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
}

func selectFeeds(feeds []catalog.Feed, host string, explicit []string) []int {
	want := map[string]bool{}
	for _, u := range explicit {
		want[catalog.CanonicalURL(u)] = true
	}
	host = strings.ToLower(strings.TrimSpace(host))
	var out []int
	for i, f := range feeds {
		if f.Status == "verified" {
			continue
		}
		u, err := url.Parse(f.URL)
		if err != nil || !strings.EqualFold(u.Host, host) {
			continue
		}
		if len(want) > 0 && !want[catalog.CanonicalURL(f.URL)] {
			continue
		}
		out = append(out, i)
	}
	return out
}

type repeatedStrings []string

func (r *repeatedStrings) String() string { return strings.Join(*r, ",") }

func (r *repeatedStrings) Set(value string) error {
	clean := strings.TrimSpace(value)
	if clean != "" {
		*r = append(*r, clean)
	}
	return nil
}

func startCallbackServer(state *captureState) (string, func(), <-chan struct{}) {
	done := make(chan struct{})
	var once sync.Once
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var payload callbackPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		state.mu.Lock()
		if payload.Status != "" {
			state.status = payload.Status
		}
		if payload.Error != "" {
			state.err = payload.Error
		}
		for _, item := range payload.CapturedFeeds {
			state.captured[item.FeedURL] = item
		}
		if payload.FeedURL != "" && payload.FeedXML != "" {
			state.captured[payload.FeedURL] = capturedFeed{FeedURL: payload.FeedURL, ContentType: payload.ContentType, FeedXML: payload.FeedXML}
		}
		terminal := payload.Status == "success" || payload.Status == "failed" || payload.Status == "aborted"
		state.mu.Unlock()
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
			if err == io.EOF {
				return "", false
			}
			return "", false
		}
		if start, ok := tok.(xml.StartElement); ok {
			return start.Name.Local, true
		}
	}
}

func die(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

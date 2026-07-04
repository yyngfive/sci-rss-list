package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"sci-rss-list/internal/catalog"
)

func TestXMLRoot(t *testing.T) {
	got, ok := xmlRoot([]byte(`<?xml version="1.0"?><rss><channel/></rss>`))
	if !ok || got != "rss" {
		t.Fatalf("xmlRoot = %q, %v", got, ok)
	}
}

func TestProtectedHints(t *testing.T) {
	if !isProtected(403, "server: cloudflare", "") {
		t.Fatal("cloudflare 403 should be protected")
	}
	if isProtected(404, "", "not found") {
		t.Fatal("404 not found should not be protected")
	}
}

func TestCachedURLSkipsFetchUnlessForced(t *testing.T) {
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(`<rss><channel/></rss>`))
	}))
	defer server.Close()

	feed := catalog.Feed{Publisher: "Test", Journal: "Cached Feed", URL: server.URL + "/feed", Status: "verified"}
	changed := false
	if errs := validateFeeds("", []catalog.Feed{feed}, time.Second, time.Second, false, &changed); len(errs) != 0 {
		t.Fatalf("verified validate errors = %v", errs)
	}
	if hits != 0 {
		t.Fatalf("verified URL was fetched %d times", hits)
	}

	if errs := validateFeeds("", []catalog.Feed{feed}, time.Second, time.Second, true, &changed); len(errs) != 0 {
		t.Fatalf("forced validate errors = %v", errs)
	}
	if hits != 1 {
		t.Fatalf("forced URL fetches = %d, want 1", hits)
	}
}

func TestPendingFeedsExcludesCachedUnlessForced(t *testing.T) {
	feeds := []catalog.Feed{
		{URL: "https://example.com/a.xml", Status: "verified"},
		{URL: "https://example.com/b.xml", Status: "protected"},
	}
	if got := len(pendingFeedIndexes(feeds, false)); got != 1 {
		t.Fatalf("pending without force = %d, want 1", got)
	}
	if got := len(pendingFeedIndexes(feeds, true)); got != 2 {
		t.Fatalf("pending with force = %d, want 2", got)
	}
}

func TestSourceDocumentedProtectedNeedsManualVerification(t *testing.T) {
	if !needsManualVerification("source_documented", fetchResult{status: "protected"}) {
		t.Fatal("source_documented protected response should be sent to manual verification")
	}
	if !needsManualVerification("protected", fetchResult{status: "protected"}) {
		t.Fatal("protected response should be sent to manual verification")
	}
	if needsManualVerification("verified", fetchResult{status: "protected"}) {
		t.Fatal("verified feeds should not silently fall back to manual verification")
	}
	if needsManualVerification("source_documented", fetchResult{status: "verified"}) {
		t.Fatal("verified XML should not need manual verification")
	}
}

func TestMarkVerifiedUpdatesJSONStatus(t *testing.T) {
	feeds := []catalog.Feed{{
		Status: "protected",
		Notes:  "Generic validator receives a protected or challenge response.",
	}}
	changed := false
	markVerified(feeds, 0, &changed)
	if !changed {
		t.Fatal("markVerified did not report a change")
	}
	if feeds[0].Status != "verified" {
		t.Fatalf("status = %q, want verified", feeds[0].Status)
	}
	if feeds[0].Notes != "" {
		t.Fatalf("notes = %q, want empty", feeds[0].Notes)
	}
}

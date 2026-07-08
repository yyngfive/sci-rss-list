package main

import (
	"os"
	"path/filepath"
	"testing"

	"sci-rss-list/internal/catalog"
)

func TestDuplicateURLCatchesExistingAndBatchDuplicates(t *testing.T) {
	existing := []catalog.Feed{{URL: "https://example.com/feed/"}}
	additions := []catalog.Feed{
		{URL: "https://example.com/other"},
		{URL: "HTTPS://EXAMPLE.COM/feed"},
	}
	if got := duplicateURL(existing, additions); got == "" {
		t.Fatal("duplicateURL missed canonical duplicate")
	}
}

func TestReadInputAcceptsSingleFeed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "feed.json")
	data := `{
  "publisher": "Test",
  "journal": "Test Journal",
  "url": "https://example.com/feed",
  "subjects": ["test"],
  "source": "https://example.com/",
  "method": "manual",
  "status": "source_documented",
  "notes": ""
}`
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
	feeds, err := readInput(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(feeds) != 1 || feeds[0].Journal != "Test Journal" {
		t.Fatalf("readInput = %#v", feeds)
	}
}

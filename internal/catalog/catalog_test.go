package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCanonicalURL(t *testing.T) {
	got := CanonicalURL(" HTTPS://Example.COM/feed/ ")
	if got != "https://example.com/feed" {
		t.Fatalf("CanonicalURL = %q", got)
	}
}

func TestKnownBrokenFeedURLsAreAbsent(t *testing.T) {
	root := filepath.Join("..", "..")
	data, err := os.ReadFile(filepath.Join(root, "data", "feeds.json"))
	if err != nil {
		t.Fatal(err)
	}
	broken := []string{
		"jc=aaomcv",
		"jc=inoraj",
		"jc=scisignal",
		"www.bmj.com/rss/current.xml",
		"7C6970A165E05FF812E16C2BCF51F02D",
		"CDFBC8AB9F96AC14CB38613F891D8F97",
		"1A08D3491E8754487EA02F99E68237DB",
	}
	text := string(data)
	for _, value := range broken {
		if strings.Contains(text, value) {
			t.Fatalf("known broken feed URL fragment still present: %s", value)
		}
	}
}

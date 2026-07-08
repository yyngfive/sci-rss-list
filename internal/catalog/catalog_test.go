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

func TestWriteReadmePublisherIndexUsesVerifiedOverTotal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "README.md")
	readme := "# Test\n\n## Publisher Index\n\nold table\n\n## Entry Format\n\nbody\n"
	if err := os.WriteFile(path, []byte(readme), 0644); err != nil {
		t.Fatal(err)
	}
	feeds := []Feed{
		{Publisher: "B Pub", Status: "verified"},
		{Publisher: "A Pub", Status: "verified"},
		{Publisher: "A Pub", Status: "protected"},
	}
	if err := WriteReadmePublisherIndex(path, feeds); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	if !strings.Contains(text, "| A Pub | 1/2 | [publishers/a-pub.md](publishers/a-pub.md) |") {
		t.Fatalf("README missing A Pub count:\n%s", text)
	}
	if !strings.Contains(text, "| B Pub | 1/1 | [publishers/b-pub.md](publishers/b-pub.md) |") {
		t.Fatalf("README missing B Pub count:\n%s", text)
	}
	if !strings.Contains(text, "## Entry Format") {
		t.Fatalf("README lost next section:\n%s", text)
	}
}

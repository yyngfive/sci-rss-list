package catalog

import "testing"

func TestCanonicalURL(t *testing.T) {
	got := CanonicalURL(" HTTPS://Example.COM/feed/ ")
	if got != "https://example.com/feed" {
		t.Fatalf("CanonicalURL = %q", got)
	}
}

//go:build windows

package main

import "testing"

func TestSameFeedURL(t *testing.T) {
	tests := []struct {
		name     string
		actual   string
		expected string
		want     bool
	}{
		{
			name:     "exact",
			actual:   "https://onlinelibrary.wiley.com/feed/14677652/most-recent",
			expected: "https://onlinelibrary.wiley.com/feed/14677652/most-recent",
			want:     true,
		},
		{
			name:     "normalizes host case and fragment",
			actual:   "https://OnlineLibrary.Wiley.com/feed/14677652/most-recent#top",
			expected: "https://onlinelibrary.wiley.com/feed/14677652/most-recent",
			want:     true,
		},
		{
			name:     "normalizes trailing slash",
			actual:   "https://onlinelibrary.wiley.com/feed/14677652/most-recent/",
			expected: "https://onlinelibrary.wiley.com/feed/14677652/most-recent",
			want:     true,
		},
		{
			name:     "keeps query significant",
			actual:   "https://pubs.acs.org/action/showFeed?type=axatoc&feed=rss&jc=aaomcv",
			expected: "https://pubs.acs.org/action/showFeed?type=axatoc&feed=rss&jc=inoraj",
			want:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sameFeedURL(tt.actual, tt.expected); got != tt.want {
				t.Fatalf("sameFeedURL() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestVisibleFeedXML(t *testing.T) {
	got, ok := visibleFeedXML("This XML file does not appear to have any style information.\n<rss><channel/></rss>")
	if !ok {
		t.Fatal("visible XML viewer text was not recognized")
	}
	if got != "<rss><channel/></rss>" {
		t.Fatalf("visibleFeedXML = %q", got)
	}
	if _, ok := visibleFeedXML("not a feed page"); ok {
		t.Fatal("plain text should not be recognized as feed XML")
	}
}

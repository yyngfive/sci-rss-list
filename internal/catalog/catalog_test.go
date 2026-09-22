package catalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func rawFor(t *testing.T, feeds []Feed) []map[string]json.RawMessage {
	t.Helper()
	b, err := json.Marshal(feeds)
	if err != nil {
		t.Fatal(err)
	}
	var raw []map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	return raw
}

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
		// Platform migrations retired these feed endpoints (2026-09-22):
		// LWW moved to Ovid (feed.aspx redirects to journal home, no RSS),
		// SSRN dropped RSS entirely, and AIP's Atypon showFeed pattern 404s.
		"OAKS.Journals/feed.aspx",
		"papers.ssrn.com/sol3/JournalRss.cfm",
		"pubs.aip.org/action/showFeed",
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

func baseFeed() Feed {
	return Feed{
		Publisher: "ACS",
		Journal:   "ACS Nano (ASAP)",
		FeedScope: "single_journal",
		URL:       "https://pubs.acs.org/rss/ancac3/asap.xml",
		Subjects:  []string{"nanoscience"},
		Source:    "https://pubs.acs.org/page/nano/rss.html",
		Method:    "publisher_index",
		Status:    "verified",
	}
}

func TestValidateShapeIdentityRules(t *testing.T) {
	cases := []struct {
		name    string
		feeds   []Feed
		wantErr string
	}{
		{
			name: "single journal with an official title",
			feeds: func() []Feed {
				f := baseFeed()
				f.CanonicalJournal = Ptr("ACS Nano")
				f.FeedType, f.FeedName = Ptr("asap"), Ptr("ASAP")
				return []Feed{f}
			}(),
		},
		{
			name:    "single journal without a canonical title",
			feeds:   []Feed{baseFeed()},
			wantErr: "single_journal requires canonical_journal",
		},
		{
			name: "aggregate feed claiming one journal",
			feeds: func() []Feed {
				f := baseFeed()
				f.FeedScope = "multi_journal"
				f.CanonicalJournal = Ptr("ACS Nano")
				return []Feed{f}
			}(),
			wantErr: `canonical_journal must be null for feed_scope "multi_journal"`,
		},
		{
			name: "subject collection without a collection",
			feeds: func() []Feed {
				f := baseFeed()
				f.FeedScope = "subject_collection"
				return []Feed{f}
			}(),
			wantErr: "subject_collection requires collection platform, id, and name",
		},
		{
			name: "collection on a single journal",
			feeds: func() []Feed {
				f := baseFeed()
				f.CanonicalJournal = Ptr("ACS Nano")
				f.Collection = &Collection{Platform: "bioRxiv", ID: "nanomaterials", Name: "Nanomaterials"}
				return []Feed{f}
			}(),
			wantErr: "collection is only for subject_collection",
		},
		{
			name: "unknown feed scope",
			feeds: func() []Feed {
				f := baseFeed()
				f.FeedScope = "journal"
				f.CanonicalJournal = Ptr("ACS Nano")
				return []Feed{f}
			}(),
			wantErr: `invalid feed_scope "journal"`,
		},
		{
			name: "unverified issn shape",
			feeds: func() []Feed {
				f := baseFeed()
				f.CanonicalJournal = Ptr("ACS Nano")
				f.IssnL = Ptr("nano-1")
				return []Feed{f}
			}(),
			wantErr: "issn_l",
		},
		{
			name: "feed type without a display name",
			feeds: func() []Feed {
				f := baseFeed()
				f.CanonicalJournal = Ptr("ACS Nano")
				f.FeedType = Ptr("asap")
				return []Feed{f}
			}(),
			wantErr: "feed_type and feed_name are set together or left null",
		},
		{
			name: "two feeds of one journal disagree on issn_l",
			feeds: func() []Feed {
				a := baseFeed()
				a.CanonicalJournal = Ptr("ACS Nano")
				a.IssnL = Ptr("1936-0851")
				b := baseFeed()
				b.Journal = "ACS Nano (Current Issue)"
				b.URL = "https://pubs.acs.org/rss/ancac3/currentIssue.xml"
				b.CanonicalJournal = Ptr("ACS Nano")
				return []Feed{a, b}
			}(),
			wantErr: `feeds of "ACS Nano" must share one issn_l`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := ValidateShape(tc.feeds, rawFor(t, tc.feeds))
			if tc.wantErr == "" {
				if len(errs) > 0 {
					t.Fatalf("unexpected errors: %v", errs)
				}
				return
			}
			if len(errs) == 0 || !strings.Contains(strings.Join(errs, "\n"), tc.wantErr) {
				t.Fatalf("errors = %v, want one mentioning %q", errs, tc.wantErr)
			}
		})
	}
}

func TestValidateShapeRequiresFeedScopeKey(t *testing.T) {
	feeds := []Feed{baseFeed()}
	raw := []map[string]json.RawMessage{{
		"publisher": json.RawMessage(`"ACS"`),
		"journal":   json.RawMessage(`"ACS Nano"`),
		"url":       json.RawMessage(`"https://pubs.acs.org/rss/ancac3/asap.xml"`),
		"subjects":  json.RawMessage(`["nanoscience"]`),
		"source":    json.RawMessage(`"https://pubs.acs.org/page/nano/rss.html"`),
		"method":    json.RawMessage(`"publisher_index"`),
		"status":    json.RawMessage(`"verified"`),
		"notes":     json.RawMessage(`""`),
		"url_extra": json.RawMessage(`"x"`),
	}}
	errs := ValidateShape(feeds, raw)
	text := strings.Join(errs, "\n")
	if !strings.Contains(text, "missing field feed_scope") {
		t.Fatalf("errors = %v, want a missing feed_scope error", errs)
	}
	if !strings.Contains(text, "unknown field url_extra") {
		t.Fatalf("errors = %v, want an unknown field error", errs)
	}
}

func TestSaveKeepsNullIdentityFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "feeds.json")
	feeds := []Feed{
		func() Feed {
			f := baseFeed()
			f.CanonicalJournal = Ptr("ACS Nano")
			f.FeedType, f.FeedName, f.IssnL = Ptr("asap"), Ptr("ASAP"), Ptr("1936-0851")
			return f
		}(),
		func() Feed {
			f := baseFeed()
			f.Publisher = "bioRxiv/medRxiv"
			f.Journal = "bioRxiv: Nanomaterials"
			f.FeedScope = "subject_collection"
			f.FeedType, f.FeedName = Ptr("subject_collection"), Ptr("Nanomaterials")
			f.Collection = &Collection{Platform: "bioRxiv", ID: "nanomaterials", Name: "Nanomaterials"}
			f.URL = "https://connect.biorxiv.org/biorxiv_xml.php?subject=nanomaterials"
			return f
		}(),
		func() Feed {
			f := baseFeed()
			f.Publisher = "Nature"
			f.Journal = "Nature Methods"
			f.CanonicalJournal = Ptr("Nature Methods")
			f.URL = "https://www.nature.com/nmeth.rss"
			return f
		}(),
	}
	if err := Save(path, feeds); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, want := range []string{`"canonical_journal": null`, `"issn_l": null`, `"feed_type": null`, `"collection": null`} {
		if !strings.Contains(text, want) {
			t.Fatalf("saved JSON lost a null field (%s):\n%s", want, text)
		}
	}
	loaded, raw, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if errs := ValidateShape(loaded, raw); len(errs) > 0 {
		t.Fatalf("round-tripped catalog does not validate: %v", errs)
	}
	if loaded[0].IssnL == nil || *loaded[0].IssnL != "1936-0851" {
		t.Fatalf("issn_l lost in round trip: %+v", loaded[0])
	}
	if loaded[1].Collection == nil || loaded[1].Collection.ID != "nanomaterials" {
		t.Fatalf("collection lost in round trip: %+v", loaded[1])
	}
}

func TestCatalogIdentityFieldsSatisfyTheContract(t *testing.T) {
	feeds, raw, err := Load(filepath.Join("..", "..", "data", "feeds.json"))
	if err != nil {
		t.Fatal(err)
	}
	if errs := ValidateShape(feeds, raw); len(errs) > 0 {
		t.Fatalf("data/feeds.json does not validate: %v", errs[:min(10, len(errs))])
	}
	byTitle := map[string]string{}
	for _, f := range feeds {
		if f.CanonicalJournal == nil {
			continue
		}
		issn := ""
		if f.IssnL != nil {
			issn = *f.IssnL
		}
		title := *f.CanonicalJournal
		if previous, ok := byTitle[title]; ok && previous != issn {
			t.Fatalf("feeds of %q carry %q and %q", title, previous, issn)
		}
		byTitle[title] = issn
	}
}

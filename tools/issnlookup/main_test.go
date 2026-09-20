package main

import (
	"strings"
	"testing"

	"sci-rss-list/internal/catalog"
)

func TestProposalMatchesAllowsAWordingVariation(t *testing.T) {
	cases := []struct {
		want, got string
		match     bool
	}{
		{"The Journal of Organic Chemistry", "Journal of Organic Chemistry", true},
		{"Crystal Growth &amp; Design", "Crystal Growth & Design", true},
		{"Physical Review A", "Physical Review", false},
		{"Cell", "Cells", false},
	}
	for _, c := range cases {
		if got := proposalMatches(c.want, c.got); got != c.match {
			t.Errorf("proposalMatches(%q, %q) = %v, want %v", c.want, c.got, got, c.match)
		}
	}
	if sameTitle("The Journal of Organic Chemistry", "Journal of Organic Chemistry") {
		t.Fatal("confirmation must not accept a title variation")
	}
}

func TestPublisherPrefixNeedsOneSharedPrefix(t *testing.T) {
	cases := []struct {
		publishers map[string]bool
		want       string
	}{
		{map[string]bool{"ACS": true}, "10.1021"},
		{map[string]bool{"Cell Press": true, "Elsevier/ScienceDirect": true}, "10.1016"},
		{map[string]bool{"Nature": true}, ""},
		{map[string]bool{"ACS": true, "Nature": true}, ""},
	}
	for _, c := range cases {
		if got := publisherPrefix(c.publishers); got != c.want {
			t.Errorf("publisherPrefix(%v) = %q, want %q", c.publishers, got, c.want)
		}
	}
}

func TestIssnsFromURLsReadsFeedPatternsThatCarryAnISSN(t *testing.T) {
	cases := []struct {
		name string
		urls []string
		want []string
	}{
		{
			name: "ScienceDirect uses the bare eight digits",
			urls: []string{"https://rss.sciencedirect.com/publication/science/00928674"},
			want: []string{"0092-8674"},
		},
		{
			name: "Wiley uses the hyphenated online ISSN",
			urls: []string{"https://onlinelibrary.wiley.com/feed/1521-4095/most-recent"},
			want: []string{"1521-4095"},
		},
		{
			name: "a check digit keeps its uppercase X",
			urls: []string{"https://onlinelibrary.wiley.com/feed/1521-409X/most-recent"},
			want: []string{"1521-409X"},
		},
		{
			name: "ACS codes are not ISSNs",
			urls: []string{"https://pubs.acs.org/rss/ancac3/asap.xml"},
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := issnsFromURLs(tc.urls)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("issnsFromURLs(%v) = %v, want %v", tc.urls, got, tc.want)
			}
		})
	}
}

func TestMembersFromWorksKeepsOnlyExactTitles(t *testing.T) {
	cases := []struct {
		name  string
		title string
		items []crossrefWork
		want  []string
	}{
		{
			name:  "cited journals are dropped",
			title: "ACS Nano",
			items: []crossrefWork{
				{ContainerTitle: []string{"Nano Letters"}, ISSN: []string{"1530-6984", "1530-6992"}},
				{ContainerTitle: []string{"ACS Nano"}, ISSN: []string{"1936-0851", "1936-086X"}},
			},
			want: []string{"1936-0851", "1936-086X"},
		},
		{
			name:  "an escaped ampersand is the same title",
			title: "Crystal Growth & Design",
			items: []crossrefWork{
				{ContainerTitle: []string{"Crystal Growth &amp; Design"}, ISSN: []string{"1528-7483"}},
			},
			want: []string{"1528-7483"},
		},
		{
			name:  "the parent journal is not the journal itself",
			title: "Physical Review A",
			items: []crossrefWork{
				{ContainerTitle: []string{"Physical Review"}, ISSN: []string{"0031-899X"}},
			},
			want: nil,
		},
		{
			name:  "repeated works contribute one ISSN each",
			title: "Cell",
			items: []crossrefWork{
				{ContainerTitle: []string{"Cell"}, ISSN: []string{"0092-8674", "1097-4172"}},
				{ContainerTitle: []string{"Cell"}, ISSN: []string{"0092-8674"}},
			},
			want: []string{"0092-8674", "1097-4172"},
		},
		{
			name:  "a substring match is not the title",
			title: "Cell",
			items: []crossrefWork{
				{ContainerTitle: []string{"Cell reports"}, ISSN: []string{"2160-7680"}},
				{ContainerTitle: []string{"Cells"}, ISSN: []string{"2073-4409"}},
			},
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := membersFromWorks(tc.items, tc.title)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("membersFromWorks() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIssnLFromPortalPage(t *testing.T) {
	cases := []struct {
		name, body, want string
	}{
		{
			name: "the record marks the linking ISSN as an attribute",
			body: `<a data-issn="1936-086X" issnl="1936-0851" class="link">`,
			want: "1936-0851",
		},
		{
			name: "the label links to the linking record",
			body: `<dt>ISSN-L:</dt><dd><a href="/resource/ISSN-L/1050-2947?issn=2469-9926">1050-2947</a></dd>`,
			want: "1050-2947",
		},
		{
			name: "a page without a linking record yields nothing",
			body: `<dt>ISSN:</dt><dd>1936-086X</dd>`,
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := issnLFromPortalPage(tc.body); got != tc.want {
				t.Fatalf("issnLFromPortalPage() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPortalRecordFromPageReadsTheRecordTitle(t *testing.T) {
	body := `<html><head><title>ISSN 0163-1829 - Physical review. B, Condensed matter</title></head>` +
		`<body><a issnl="0163-1829">0163-1829</a></body></html>`
	got := portalRecordFromPage(body)
	if got.issnL != "0163-1829" {
		t.Fatalf("issnL = %q", got.issnL)
	}
	if got.title != "Physical review. B, Condensed matter" {
		t.Fatalf("title = %q", got.title)
	}
	if portalRecordFromPage(`<html><head><title>ISSN Portal</title></head>`).title != "" {
		t.Fatal("a page with no record title must yield nothing")
	}
}

func TestTitleGroupMatchesKeepsTheEditionOfTheCatalogTitle(t *testing.T) {
	cases := []struct {
		name        string
		e           entry
		recordTitle string
		want        bool
	}{
		{
			name:        "a punctuation variant is the same title",
			e:           entry{title: "Physical Review B", titles: []string{"Physical Review B"}},
			recordTitle: "Physical review. B",
			want:        true,
		},
		{
			name:        "a superseded title with a subtitle is another record",
			e:           entry{title: "Physical Review B", titles: []string{"Physical Review B"}},
			recordTitle: "Physical review. B, Condensed matter",
		},
		{
			name:        "another language edition is not this journal",
			e:           entry{title: "Environmental Health Perspectives", titles: []string{"Environmental Health Perspectives"}},
			recordTitle: "Huanjing yu jiankang zhanwang",
		},
		{
			name:        "a section feed still stands for its parent title",
			e:           entry{title: "Physical Review B", titles: []string{"Physical Review B", "Physical Review B. Condensed Matter"}},
			recordTitle: "Physical review. B, Condensed matter",
			want:        true,
		},
		{
			name:        "an edition qualifier does not make another journal",
			e:           entry{title: "Optica"},
			recordTitle: "Optica (Online)",
			want:        true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := titleGroupMatches(tc.e, tc.recordTitle); got != tc.want {
				t.Fatalf("titleGroupMatches() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTitleOfUsesThePlainFeedLabelOfItsOwnJournal(t *testing.T) {
	cases := []struct {
		name string
		in   catalog.Feed
		want string
	}{
		{
			name: "a plain single feed is subscribed under its own label",
			in:   catalog.Feed{Journal: "Physical Review B", CanonicalJournal: catalog.Ptr("Physical Review B"), FeedScope: scopeSingle},
			want: "Physical Review B",
		},
		{
			name: "a labeled section feed keeps the parent title",
			in:   catalog.Feed{Journal: "Physical Review B: Semiconductors I: bulk", CanonicalJournal: catalog.Ptr("Physical Review B"), FeedScope: scopeSingle, FeedType: catalog.Ptr("toc_section")},
			want: "Physical Review B",
		},
		{
			name: "a collection feed has no journal title",
			in:   catalog.Feed{Journal: "bioRxiv: Cell Biology", FeedScope: "subject_collection"},
			want: "bioRxiv: Cell Biology",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := titleOf(tc.in); got != tc.want {
				t.Fatalf("titleOf() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAgreedIssnLRejectsDisagreement(t *testing.T) {
	if got := agreedIssnL([]string{"1936-0851", "1936-0851"}); len(got) != 1 || got[0] != "1936-0851" {
		t.Fatalf("agreedIssnL() = %v, want one value", got)
	}
	if got := agreedIssnL([]string{"1050-2947", "2469-9926"}); len(got) != 2 {
		t.Fatalf("different ISSN-L values must stay visible: %v", got)
	}
	if got := agreedIssnL([]string{"", "not-an-issn"}); len(got) != 0 {
		t.Fatalf("agreedIssnL() = %v, want nothing", got)
	}
}

func TestPublisherMatchesRejectsAnotherPublisher(t *testing.T) {
	cases := []struct {
		hint, found string
		want        bool
	}{
		{"", "Elsevier", true},
		{"American Chemical Society", "American Chemical Society", true},
		{"National Academy of Sciences", "Proceedings of the National Academy of Sciences", true},
		{"American Chemical Society", "Portland Press", false},
		{"American Physical Society", "American Institute of Physics", false},
	}
	for _, c := range cases {
		if got := publisherMatches(c.hint, c.found); got != c.want {
			t.Errorf("publisherMatches(%q, %q) = %v, want %v", c.hint, c.found, got, c.want)
		}
	}
}

func TestPublisherHintOnlyNarrowsASinglePublisherGroup(t *testing.T) {
	if got := publisherHint(map[string]bool{"ACS": true}); got != "American Chemical Society" {
		t.Fatalf("publisherHint(ACS) = %q", got)
	}
	if got := publisherHint(map[string]bool{"Cell Press": true, "Elsevier/ScienceDirect": true}); got != "" {
		t.Fatalf("publisherHint(mixed) = %q, want no hint", got)
	}
	if got := publisherHint(map[string]bool{"Nature": true}); got != "" {
		t.Fatalf("publisherHint(Nature) = %q, want no hint", got)
	}
}

func TestNormalizeTitle(t *testing.T) {
	cases := [][2]string{
		{"Physical Review Letters", "physical review letters"},
		{"Journal of Near Infrared Spectroscopy", "journal of near infrared spectroscopy"},
		{"ACS ES&T Water", "acs es and t water"},
		{"Crystal Growth &amp; Design", "crystal growth and design"},
		{"Light: Science &amp; Applications", "light science and applications"},
		{"  Optica   Quantum  ", "optica quantum"},
	}
	for _, c := range cases {
		if got := normalizeTitle(c[0]); got != c[1] {
			t.Errorf("normalizeTitle(%q) = %q, want %q", c[0], got, c[1])
		}
	}
}

func TestPendingEntriesOnlyReturnsSharedJournalsWithoutIssnL(t *testing.T) {
	feeds := []catalog.Feed{
		{Publisher: "ACS", Journal: "ACS Nano (ASAP)", CanonicalJournal: catalog.Ptr("ACS Nano")},
		{Publisher: "ACS", Journal: "ACS Nano (Current Issue)", CanonicalJournal: catalog.Ptr("ACS Nano")},
		{Publisher: "Nature", Journal: "Nature Methods", CanonicalJournal: catalog.Ptr("Nature Methods")},
		{Publisher: "bioRxiv/medRxiv", Journal: "medRxiv: Health Economics"},
	}
	entries := pendingEntries(feeds)
	if len(entries) != 1 {
		t.Fatalf("entries = %v, want only ACS Nano", entries)
	}
	if entries[0].feeds != 2 || !entries[0].publishers["ACS"] {
		t.Fatalf("entry = %+v", entries[0])
	}

	feeds[0].IssnL = catalog.Ptr("1936-0851")
	if entries := pendingEntries(feeds); len(entries) != 0 {
		t.Fatalf("a journal with a known ISSN-L should not be looked up again: %v", entries)
	}
}

func TestPropagateIssnLSharesOneIdentityAcrossFeeds(t *testing.T) {
	feeds := []catalog.Feed{
		{Publisher: "ACS", Journal: "ACS Nano (ASAP)", CanonicalJournal: catalog.Ptr("ACS Nano"), IssnL: catalog.Ptr("1936-0851")},
		{Publisher: "ACS", Journal: "ACS Nano (Current Issue)", CanonicalJournal: catalog.Ptr("ACS Nano")},
		{Publisher: "Nature", Journal: "Nature Methods", CanonicalJournal: catalog.Ptr("Nature Methods")},
	}
	if changed := propagateIssnL(feeds); changed != 1 {
		t.Fatalf("changed = %d, want 1", changed)
	}
	if feeds[1].IssnL == nil || *feeds[1].IssnL != "1936-0851" {
		t.Fatalf("current issue feed did not inherit the ISSN-L: %+v", feeds[1])
	}
	if feeds[2].IssnL != nil {
		t.Fatalf("a single feed journal must stay null: %v", *feeds[2].IssnL)
	}
}

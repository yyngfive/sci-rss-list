package main

import (
	"testing"

	"sci-rss-list/internal/catalog"
)

func feed(publisher, journal, url string) catalog.Feed {
	return catalog.Feed{Publisher: publisher, Journal: journal, URL: url}
}

func identity(t *testing.T, f catalog.Feed) (scope, canonical, feedType, feedName, collection string) {
	t.Helper()
	scope = f.FeedScope
	canonical = opt(f.CanonicalJournal)
	feedType = opt(f.FeedType)
	feedName = opt(f.FeedName)
	if f.Collection != nil {
		collection = f.Collection.Platform + "/" + f.Collection.ID + "/" + f.Collection.Name
	}
	return
}

func opt(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func TestDerive(t *testing.T) {
	cases := []struct {
		name                       string
		in                         catalog.Feed
		scope, canonical, feedType string
		feedName, collection       string
	}{
		{
			name:      "plain single journal",
			in:        feed("Nature", "Nature Methods", "https://www.nature.com/nmeth.rss"),
			scope:     "single_journal",
			canonical: "Nature Methods",
		},
		{
			name:      "acs asap keeps the legacy label but shares the journal",
			in:        feed("ACS", "Journal of the American Chemical Society (ASAP)", "https://pubs.acs.org/rss/jacsat/asap.xml"),
			scope:     "single_journal",
			canonical: "Journal of the American Chemical Society",
			feedType:  "asap",
			feedName:  "ASAP",
		},
		{
			name:      "acs current issue",
			in:        feed("ACS", "ACS Nano (Current Issue)", "https://pubs.acs.org/rss/ancac3/currentIssue.xml"),
			scope:     "single_journal",
			canonical: "ACS Nano",
			feedType:  "current_issue",
			feedName:  "Current Issue",
		},
		{
			name:      "aps recently published",
			in:        feed("APS", "Physical Review Letters (Recently Published)", "https://feeds.aps.org/rss/recent/prl.xml"),
			scope:     "single_journal",
			canonical: "Physical Review Letters",
			feedType:  "recently_published",
			feedName:  "Recently Published",
		},
		{
			name:      "aps editors suggestions served under recent",
			in:        feed("APS", "Physical Review Letters (Editors' Suggestions)", "https://feeds.aps.org/rss/recent/prlsuggestions.xml"),
			scope:     "single_journal",
			canonical: "Physical Review Letters",
			feedType:  "editors_suggestions",
			feedName:  "Editors' Suggestions",
		},
		{
			name:      "aps section keeps the second colon inside the section name",
			in:        feed("APS", "Physical Review B: Semiconductors I: bulk", "https://feeds.aps.org/rss/tocsec/PRB-SemiconductorsIbulk.xml"),
			scope:     "single_journal",
			canonical: "Physical Review B",
			feedType:  "toc_section",
			feedName:  "Semiconductors I: bulk",
		},
		{
			name:     "aps cross-journal aggregate is not a journal",
			in:       feed("APS", "APS Journals (All Editors' Suggestions)", "https://feeds.aps.org/rss/allsuggestions.xml"),
			scope:    "multi_journal",
			feedType: "editors_suggestions",
			feedName: "All Editors' Suggestions",
		},
		{
			name:      "journal title containing a colon is left intact",
			in:        feed("Elsevier/ScienceDirect", "Physica A: Statistical Mechanics and its Applications", "https://rss.sciencedirect.com/publication/science/03784371"),
			scope:     "single_journal",
			canonical: "Physica A: Statistical Mechanics and its Applications",
		},
		{
			name:      "pnas topic feed maps to the parent journal",
			in:        feed("PNAS", "PNAS: Chemical Sciences", "https://www.pnas.org/action/showFeed?type=searchTopic&feed=rss&taxonomyCode=topic&tagCode=chem"),
			scope:     "single_journal",
			canonical: "Proceedings of the National Academy of Sciences",
			feedType:  "toc_section",
			feedName:  "Chemical Sciences",
		},
		{
			name:      "pnas main feed has no feed type",
			in:        feed("PNAS", "Proceedings of the National Academy of Sciences", "https://www.pnas.org/action/showFeed?type=etoc&feed=rss&jc=PNAS"),
			scope:     "single_journal",
			canonical: "Proceedings of the National Academy of Sciences",
		},
		{
			name:       "biorxiv subject collection",
			in:         feed("bioRxiv/medRxiv", "bioRxiv: Cell Biology", "https://connect.biorxiv.org/biorxiv_xml.php?subject=cell_biology"),
			scope:      "subject_collection",
			feedType:   "subject_collection",
			feedName:   "Cell Biology",
			collection: "bioRxiv/cell_biology/Cell Biology",
		},
		{
			name:       "medrxiv subject collection",
			in:         feed("bioRxiv/medRxiv", "medRxiv: Health Economics", "https://connect.medrxiv.org/medrxiv_xml.php?subject=health_economics"),
			scope:      "subject_collection",
			feedType:   "subject_collection",
			feedName:   "Health Economics",
			collection: "medRxiv/health_economics/Health Economics",
		},
		{
			name:      "chemrxiv latest",
			in:        feed("ChemRxiv", "ChemRxiv: Latest", "https://chemrxiv.org/action/showFeed?type=latest&format=rss"),
			scope:     "single_journal",
			canonical: "ChemRxiv",
			feedType:  "latest_preprints",
			feedName:  "Latest",
		},
		{
			name:      "publisher lowercase title keeps its registered casing",
			in:        feed("Cambridge Core", "animal", "https://www.cambridge.org/core/rss/product/id/7CC89400BC979479B6AE7BDEF995211F"),
			scope:     "single_journal",
			canonical: "animal",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := tc.in
			if err := derive(&f); err != nil {
				t.Fatal(err)
			}
			scope, canonical, feedType, feedName, collection := identity(t, f)
			if scope != tc.scope || canonical != tc.canonical || feedType != tc.feedType || feedName != tc.feedName || collection != tc.collection {
				t.Fatalf("derived\n  scope=%q canonical=%q type=%q name=%q collection=%q\nwant\n  scope=%q canonical=%q type=%q name=%q collection=%q",
					scope, canonical, feedType, feedName, collection, tc.scope, tc.canonical, tc.feedType, tc.feedName, tc.collection)
			}
			if tc.scope != "multi_journal" && tc.scope != "subject_collection" && canonical == "" {
				t.Fatalf("single_journal entry has no canonical journal")
			}
			if f.Journal != tc.in.Journal {
				t.Fatalf("legacy journal label changed from %q to %q", tc.in.Journal, f.Journal)
			}
			if f.IssnL != nil {
				t.Fatalf("migration must not invent issn_l, got %q", *f.IssnL)
			}
			again := f
			if err := derive(&again); err != nil {
				t.Fatal(err)
			}
			if again.FeedScope != f.FeedScope || opt(again.CanonicalJournal) != canonical {
				t.Fatal("derive is not idempotent")
			}
		})
	}
}

func TestDeriveRejectsUnrecognizedPublisherFeed(t *testing.T) {
	f := feed("APS", "Physical Review Focus Newsletter", "https://feeds.aps.org/rss/focus.xml")
	if err := derive(&f); err == nil {
		t.Fatalf("expected an error for an unmapped APS feed, got scope %q", f.FeedScope)
	}
	rxiv := feed("bioRxiv/medRxiv", "bioRxiv Everything", "https://connect.biorxiv.org/biorxiv_xml.php")
	if err := derive(&rxiv); err == nil {
		t.Fatalf("expected an error for a subject-less rxiv feed, got scope %q", rxiv.FeedScope)
	}
}

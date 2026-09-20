// Command migrateidentity fills the journal and feed identity fields for every
// catalog entry from the publisher-specific label and URL rules recorded here.
// It is idempotent and never rewrites the legacy journal label.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"sci-rss-list/internal/catalog"
)

const (
	scopeSingle  = "single_journal"
	scopeMulti   = "multi_journal"
	scopeSubject = "subject_collection"
)

// labelFeedTypes maps a legacy journal label suffix to its feed identity.
var labelFeedTypes = []struct {
	suffix   string
	feedType string
	feedName string
}{
	{"(ASAP)", "asap", "ASAP"},
	{"(Current Issue)", "current_issue", "Current Issue"},
	{"(Recently Published)", "recently_published", "Recently Published"},
	{"(Recently Accepted)", "recently_accepted", "Recently Accepted"},
	{"(Editors' Suggestions)", "editors_suggestions", "Editors' Suggestions"},
}

func main() {
	dataPath := flag.String("data", filepath.Join("data", "feeds.json"), "catalog JSON path")
	dryRun := flag.Bool("dry-run", false, "report the derived fields without writing")
	flag.Parse()

	feeds, _, err := catalog.Load(*dataPath)
	if err != nil {
		die(err)
	}
	for i := range feeds {
		if err := derive(&feeds[i]); err != nil {
			die(fmt.Errorf("entry %d (%s): %w", i+1, feeds[i].Journal, err))
		}
	}
	raw, err := rawShape(feeds)
	if err != nil {
		die(err)
	}
	if errs := catalog.ValidateShape(feeds, raw); len(errs) > 0 {
		for _, e := range errs[:min(len(errs), 20)] {
			fmt.Fprintln(os.Stderr, e)
		}
		die(fmt.Errorf("%d validation error(s) after migration", len(errs)))
	}
	report(feeds)
	if *dryRun {
		fmt.Println("dry run: no file written")
		return
	}
	if err := catalog.Save(*dataPath, feeds); err != nil {
		die(err)
	}
	fmt.Printf("ok: wrote identity fields for %d entries\n", len(feeds))
}

func derive(f *catalog.Feed) error {
	f.CanonicalJournal = nil
	f.IssnL = nil
	f.FeedType = nil
	f.FeedName = nil
	f.Collection = nil

	switch f.Publisher {
	case "bioRxiv/medRxiv":
		return deriveRxiv(f)
	case "PNAS":
		return derivePNAS(f)
	case "ChemRxiv":
		return deriveChemRxiv(f)
	case "APS":
		return deriveAPS(f)
	}
	if journal, feedType, feedName, ok := labeledFeed(f.Journal); ok {
		return setSingle(f, journal, feedType, feedName)
	}
	return setSingle(f, f.Journal, "", "")
}

// labeledFeed resolves a "<Journal> (<Feed Type>)" label such as the ACS and APS ones.
func labeledFeed(label string) (journal, feedType, feedName string, ok bool) {
	for _, t := range labelFeedTypes {
		if strings.HasSuffix(label, " "+t.suffix) {
			return strings.TrimSuffix(label, " "+t.suffix), t.feedType, t.feedName, true
		}
	}
	return "", "", "", false
}

func deriveRxiv(f *catalog.Feed) error {
	platform, id, ok := rxivCollection(f.URL)
	if !ok {
		return fmt.Errorf("expected a connect.*rxiv.org subject feed, got %s", f.URL)
	}
	name, ok := afterFirstColon(f.Journal)
	if !ok {
		return fmt.Errorf("expected a subject name in label %q", f.Journal)
	}
	f.FeedScope = scopeSubject
	f.FeedType = catalog.Ptr("subject_collection")
	f.FeedName = catalog.Ptr(name)
	f.Collection = &catalog.Collection{Platform: platform, ID: id, Name: name}
	return nil
}

func rxivCollection(rawurl string) (platform, id string, ok bool) {
	platform = "medRxiv"
	switch {
	case strings.Contains(rawurl, "connect.biorxiv.org/"):
		platform = "bioRxiv"
	case strings.Contains(rawurl, "connect.medrxiv.org/"):
	default:
		return "", "", false
	}
	_, rest, found := strings.Cut(rawurl, "subject=")
	if !found || rest == "" {
		return "", "", false
	}
	return platform, rest, true
}

const pnasJournal = "Proceedings of the National Academy of Sciences"

// derivePNAS treats a topic feed as the journal's own subject collection, the same
// way a bioRxiv or medRxiv subject feed is one, so a client never has to decide
// whether a classification is a journal.
func derivePNAS(f *catalog.Feed) error {
	if !strings.Contains(f.URL, "type=searchTopic") {
		return setSingle(f, pnasJournal, "", "")
	}
	id, ok := pnasTopicID(f.URL)
	if !ok {
		return fmt.Errorf("expected a taxonomy tagCode in the PNAS topic feed %s", f.URL)
	}
	name, ok := afterFirstColon(f.Journal)
	if !ok || !strings.HasPrefix(f.Journal, "PNAS:") {
		return fmt.Errorf("expected a PNAS topic feed label, got %q", f.Journal)
	}
	f.FeedScope = scopeSubject
	f.FeedType = catalog.Ptr("subject_collection")
	f.FeedName = catalog.Ptr(name)
	f.Collection = &catalog.Collection{Platform: "PNAS", ID: id, Name: name}
	return nil
}

func pnasTopicID(rawurl string) (string, bool) {
	_, rest, found := strings.Cut(rawurl, "tagCode=")
	if !found {
		return "", false
	}
	value, _, _ := strings.Cut(rest, "&")
	if value == "" {
		return "", false
	}
	return value, true
}

func deriveChemRxiv(f *catalog.Feed) error {
	if !strings.Contains(f.URL, "type=latest") {
		return fmt.Errorf("expected a ChemRxiv latest feed, got %s", f.URL)
	}
	name, ok := afterFirstColon(f.Journal)
	if !ok {
		return fmt.Errorf("expected a feed name in label %q", f.Journal)
	}
	return setSingle(f, "ChemRxiv", "latest_preprints", name)
}

func deriveAPS(f *catalog.Feed) error {
	if strings.Contains(f.URL, "/rss/allsuggestions.xml") {
		f.FeedScope = scopeMulti
		f.FeedType = catalog.Ptr("editors_suggestions")
		f.FeedName = catalog.Ptr("All Editors' Suggestions")
		return nil
	}
	if strings.Contains(f.URL, "/rss/tocsec/") {
		journal, section, ok := strings.Cut(f.Journal, ": ")
		if !ok {
			return fmt.Errorf("expected an APS section feed label, got %q", f.Journal)
		}
		return setSingle(f, journal, "toc_section", section)
	}
	journal, feedType, feedName, ok := labeledFeed(f.Journal)
	if !ok {
		return fmt.Errorf("unrecognized APS label %q", f.Journal)
	}
	return setSingle(f, journal, feedType, feedName)
}

func setSingle(f *catalog.Feed, canonical, feedType, feedName string) error {
	canonical = strings.TrimSpace(canonical)
	if canonical == "" {
		return fmt.Errorf("empty canonical journal for label %q", f.Journal)
	}
	f.FeedScope = scopeSingle
	f.CanonicalJournal = catalog.Ptr(canonical)
	if feedType != "" {
		f.FeedType = catalog.Ptr(feedType)
		f.FeedName = catalog.Ptr(strings.TrimSpace(feedName))
	}
	return nil
}

func afterFirstColon(label string) (string, bool) {
	_, rest, ok := strings.Cut(label, ": ")
	return strings.TrimSpace(rest), ok
}

func report(feeds []catalog.Feed) {
	byScope := map[string]int{}
	byType := map[string]int{}
	renamed := 0
	for _, f := range feeds {
		byScope[f.FeedScope]++
		if f.FeedType != nil {
			byType[*f.FeedType]++
		} else {
			byType["<null>"]++
		}
		if f.CanonicalJournal != nil && *f.CanonicalJournal != f.Journal {
			renamed++
		}
	}
	fmt.Printf("entries: %d\n", len(feeds))
	for _, k := range sortedKeys(byScope) {
		fmt.Printf("  scope %-19s %d\n", k, byScope[k])
	}
	for _, k := range sortedKeys(byType) {
		fmt.Printf("  type  %-19s %d\n", k, byType[k])
	}
	fmt.Printf("  canonical_journal differs from the legacy label: %d\n", renamed)
}

func sortedKeys(counts map[string]int) []string {
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func rawShape(feeds []catalog.Feed) ([]map[string]json.RawMessage, error) {
	b, err := json.Marshal(feeds)
	if err != nil {
		return nil, err
	}
	var raw []map[string]json.RawMessage
	return raw, json.Unmarshal(b, &raw)
}

func die(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

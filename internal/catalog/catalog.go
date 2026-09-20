package catalog

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	legacyFields   = []string{"publisher", "journal", "url", "subjects", "source", "method", "status", "notes"}
	identityFields = []string{"canonical_journal", "issn_l", "feed_scope", "feed_type", "feed_name", "collection"}
	fields         = append(append([]string{}, legacyFields...), identityFields...)
	// feed_scope is required; the other identity fields may be omitted or null.
	required = append(append([]string{}, legacyFields...), "feed_scope")
	methods  = map[string]bool{"publisher_index": true, "url_pattern": true, "manual": true}
	statuses = map[string]bool{"verified": true, "protected": true, "source_documented": true}

	feedScopes      = map[string]bool{"single_journal": true, "multi_journal": true, "subject_collection": true, "platform_collection": true}
	issnPattern     = regexp.MustCompile(`^\d{4}-\d{3}[0-9X]$`)
	feedTypePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

// Collection identifies a subject classification that a feed publishes. Only
// subject_collection feeds use it, so such a feed never pretends to be a journal.
type Collection struct {
	Platform string `json:"platform"`
	ID       string `json:"id"`
	Name     string `json:"name"`
}

type Feed struct {
	Publisher string `json:"publisher"`
	// Journal stays the legacy subscription label for older clients.
	Journal          string      `json:"journal"`
	CanonicalJournal *string     `json:"canonical_journal"`
	IssnL            *string     `json:"issn_l"`
	FeedScope        string      `json:"feed_scope"`
	FeedType         *string     `json:"feed_type"`
	FeedName         *string     `json:"feed_name"`
	Collection       *Collection `json:"collection"`
	URL              string      `json:"url"`
	Subjects         []string    `json:"subjects"`
	Source           string      `json:"source"`
	Method           string      `json:"method"`
	Status           string      `json:"status"`
	Notes            string      `json:"notes"`
}

func Load(path string) ([]Feed, []map[string]json.RawMessage, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var feeds []Feed
	if err := json.Unmarshal(b, &feeds); err != nil {
		return nil, nil, err
	}
	var raw []map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, nil, err
	}
	return feeds, raw, nil
}

func Save(path string, feeds []Feed) error {
	b, err := json.MarshalIndent(feeds, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0644)
}

func ValidateShape(feeds []Feed, raw []map[string]json.RawMessage) []string {
	var errs []string
	seen := map[string]int{}
	for _, f := range feeds {
		seen[CanonicalURL(f.URL)]++
	}
	for i, f := range feeds {
		n := i + 1
		for _, name := range required {
			if _, ok := raw[i][name]; !ok {
				errs = append(errs, fmt.Sprintf("entry %d: missing field %s", n, name))
			}
		}
		for name := range raw[i] {
			if !contains(fields, name) {
				errs = append(errs, fmt.Sprintf("entry %d: unknown field %s", n, name))
			}
		}
		if blank(f.Publisher) || blank(f.Journal) || blank(f.URL) || blank(f.Source) {
			errs = append(errs, fmt.Sprintf("entry %d: publisher, journal, url, and source are required", n))
		}
		if len(f.Subjects) == 0 {
			errs = append(errs, fmt.Sprintf("entry %d: subjects must be non-empty", n))
		}
		for _, s := range f.Subjects {
			if blank(s) {
				errs = append(errs, fmt.Sprintf("entry %d: subjects must not contain blanks", n))
			}
		}
		if !methods[f.Method] {
			errs = append(errs, fmt.Sprintf("entry %d: invalid method %q", n, f.Method))
		}
		if !statuses[f.Status] {
			errs = append(errs, fmt.Sprintf("entry %d: invalid status %q", n, f.Status))
		}
		errs = append(errs, validateIdentity(n, f)...)
		if !strings.HasPrefix(f.URL, "http://") && !strings.HasPrefix(f.URL, "https://") {
			errs = append(errs, fmt.Sprintf("entry %d: url must be http(s)", n))
		}
		if !strings.HasPrefix(f.Source, "http://") && !strings.HasPrefix(f.Source, "https://") {
			errs = append(errs, fmt.Sprintf("entry %d: source must be http(s)", n))
		}
		if seen[CanonicalURL(f.URL)] > 1 {
			errs = append(errs, fmt.Sprintf("entry %d: duplicate url %s", n, f.URL))
		}
	}
	errs = append(errs, validateSharedIssnL(feeds)...)
	return errs
}

func validateIdentity(n int, f Feed) []string {
	var errs []string
	if !feedScopes[f.FeedScope] {
		errs = append(errs, fmt.Sprintf("entry %d: invalid feed_scope %q", n, f.FeedScope))
	}
	switch f.FeedScope {
	case "single_journal":
		if f.CanonicalJournal == nil || blank(*f.CanonicalJournal) {
			errs = append(errs, fmt.Sprintf("entry %d: single_journal requires canonical_journal", n))
		}
	case "multi_journal", "subject_collection", "platform_collection":
		if f.CanonicalJournal != nil {
			errs = append(errs, fmt.Sprintf("entry %d: canonical_journal must be null for feed_scope %q", n, f.FeedScope))
		}
	}
	if f.IssnL != nil && !issnPattern.MatchString(*f.IssnL) {
		errs = append(errs, fmt.Sprintf("entry %d: issn_l %q must be a verified ISSN-L (NNNN-NNNC)", n, *f.IssnL))
	}
	if (f.FeedType == nil) != (f.FeedName == nil) {
		errs = append(errs, fmt.Sprintf("entry %d: feed_type and feed_name are set together or left null", n))
	}
	if f.FeedType != nil && !feedTypePattern.MatchString(*f.FeedType) {
		errs = append(errs, fmt.Sprintf("entry %d: invalid feed_type %q", n, *f.FeedType))
	}
	if f.FeedName != nil && blank(*f.FeedName) {
		errs = append(errs, fmt.Sprintf("entry %d: feed_name must not be blank when set", n))
	}
	if f.FeedScope == "subject_collection" {
		if f.Collection == nil {
			errs = append(errs, fmt.Sprintf("entry %d: subject_collection requires collection platform, id, and name", n))
		} else if blank(f.Collection.Platform) || blank(f.Collection.ID) || blank(f.Collection.Name) {
			errs = append(errs, fmt.Sprintf("entry %d: collection platform, id, and name are required", n))
		}
	} else if f.Collection != nil {
		errs = append(errs, fmt.Sprintf("entry %d: collection is only for subject_collection", n))
	}
	return errs
}

// validateSharedIssnL keeps every feed of one journal on a single identity.
func validateSharedIssnL(feeds []Feed) []string {
	type group struct {
		issn string
		rows []int
	}
	groups := map[string]*group{}
	for i, f := range feeds {
		if f.CanonicalJournal == nil {
			continue
		}
		key := strings.TrimSpace(*f.CanonicalJournal)
		issn := ""
		if f.IssnL != nil {
			issn = *f.IssnL
		}
		g := groups[key]
		if g == nil {
			g = &group{issn: issn}
			groups[key] = g
		}
		if g.issn != issn {
			g.rows = append(g.rows, i+1)
		}
	}
	var errs []string
	for key, g := range groups {
		for _, n := range g.rows {
			errs = append(errs, fmt.Sprintf("entry %d: feeds of %q must share one issn_l", n, key))
		}
	}
	return errs
}

func WritePublisherMarkdown(dir string, feeds []Feed) error {
	grouped := map[string][]Feed{}
	for _, f := range feeds {
		grouped[f.Publisher] = append(grouped[f.Publisher], f)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	old, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		return err
	}
	for _, path := range old {
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	publishers := make([]string, 0, len(grouped))
	for publisher := range grouped {
		publishers = append(publishers, publisher)
	}
	sort.Strings(publishers)
	for _, publisher := range publishers {
		rows := grouped[publisher]
		sort.Slice(rows, func(i, j int) bool { return feedSortKey(rows[i]) < feedSortKey(rows[j]) })
		var b strings.Builder
		fmt.Fprintf(&b, "# %s\n\n%d feeds generated from `data/feeds.json`.\n\n", publisher, len(rows))
		b.WriteString("| Journal | Feed Type | Subjects | Status | Method | Feed | Source | Notes |\n")
		b.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- |\n")
		for _, f := range rows {
			feedType := "—"
			if f.FeedType != nil {
				feedType = *f.FeedType
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | [RSS](%s) | [source](%s) | %s |\n",
				cell(f.Journal), feedType, cell(strings.Join(f.Subjects, ", ")), f.Status, f.Method, f.URL, f.Source, cell(f.Notes))
		}
		if err := os.WriteFile(filepath.Join(dir, Slugify(publisher)+".md"), []byte(b.String()), 0644); err != nil {
			return err
		}
	}
	return nil
}

func WriteReadmePublisherIndex(path string, feeds []Feed) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := string(b)
	start := strings.Index(text, "## Publisher Index")
	if start < 0 {
		return fmt.Errorf("README publisher index heading not found")
	}
	afterStart := text[start+len("## Publisher Index"):]
	next := strings.Index(afterStart, "\n## ")
	if next < 0 {
		return fmt.Errorf("README next section after publisher index not found")
	}
	var section strings.Builder
	section.WriteString("## Publisher Index\n\n")
	section.WriteString("| Publisher | Feeds | Page |\n")
	section.WriteString("| --- | ---: | --- |\n")
	for _, row := range publisherRows(feeds) {
		fmt.Fprintf(&section, "| %s | %d/%d | [publishers/%s.md](publishers/%s.md) |\n",
			cell(row.publisher), row.verified, row.total, row.slug, row.slug)
	}
	updated := text[:start] + section.String() + afterStart[next:]
	return os.WriteFile(path, []byte(updated), 0644)
}

type publisherRow struct {
	publisher string
	slug      string
	verified  int
	total     int
}

func publisherRows(feeds []Feed) []publisherRow {
	rowsByPublisher := map[string]*publisherRow{}
	for _, f := range feeds {
		row := rowsByPublisher[f.Publisher]
		if row == nil {
			row = &publisherRow{publisher: f.Publisher, slug: Slugify(f.Publisher)}
			rowsByPublisher[f.Publisher] = row
		}
		row.total++
		if f.Status == "verified" {
			row.verified++
		}
	}
	rows := make([]publisherRow, 0, len(rowsByPublisher))
	for _, row := range rowsByPublisher {
		rows = append(rows, *row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].publisher < rows[j].publisher })
	return rows
}

// feedSortKey keeps the feeds of one journal adjacent in generated pages.
func feedSortKey(f Feed) string {
	name := f.Journal
	if f.CanonicalJournal != nil {
		name = *f.CanonicalJournal
	}
	return strings.ToLower(name) + "\x00" + strings.ToLower(f.Journal)
}

func Ptr(s string) *string { return &s }

func CanonicalURL(rawurl string) string {
	u, err := url.Parse(strings.TrimSpace(rawurl))
	if err != nil {
		return strings.TrimSpace(rawurl)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""
	if u.Path != "/" {
		u.Path = strings.TrimRight(u.Path, "/")
	}
	return u.String()
}

func Slugify(s string) string {
	re := regexp.MustCompile(`[^a-z0-9]+`)
	slug := strings.Trim(re.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if slug == "" {
		return "publisher"
	}
	return slug
}

func PublisherCount(feeds []Feed) int {
	seen := map[string]bool{}
	for _, f := range feeds {
		seen[f.Publisher] = true
	}
	return len(seen)
}

func cell(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}

func blank(s string) bool {
	return strings.TrimSpace(s) == ""
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

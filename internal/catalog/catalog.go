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
	fields   = []string{"publisher", "journal", "url", "subjects", "source", "method", "status", "notes"}
	methods  = map[string]bool{"publisher_index": true, "url_pattern": true, "manual": true}
	statuses = map[string]bool{"verified": true, "protected": true, "source_documented": true}
)

type Feed struct {
	Publisher string   `json:"publisher"`
	Journal   string   `json:"journal"`
	URL       string   `json:"url"`
	Subjects  []string `json:"subjects"`
	Source    string   `json:"source"`
	Method    string   `json:"method"`
	Status    string   `json:"status"`
	Notes     string   `json:"notes"`
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
		for _, name := range fields {
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
		sort.Slice(rows, func(i, j int) bool { return strings.ToLower(rows[i].Journal) < strings.ToLower(rows[j].Journal) })
		var b strings.Builder
		fmt.Fprintf(&b, "# %s\n\n%d feeds generated from `data/feeds.json`.\n\n", publisher, len(rows))
		b.WriteString("| Journal | Subjects | Status | Method | Feed | Source | Notes |\n")
		b.WriteString("| --- | --- | --- | --- | --- | --- | --- |\n")
		for _, f := range rows {
			fmt.Fprintf(&b, "| %s | %s | %s | %s | [RSS](%s) | [source](%s) | %s |\n",
				cell(f.Journal), cell(strings.Join(f.Subjects, ", ")), f.Status, f.Method, f.URL, f.Source, cell(f.Notes))
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

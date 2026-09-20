// Command issnlookup resolves ISSN-L values for the journals that have more than
// one feed in the catalog, so those feeds share one stable identity. Titles it
// cannot confirm stay null; nothing is guessed.
//
// Discovery only proposes ISSNs that belong to the journal: a Crossref work whose
// container title repeats the catalog title, or a Wikidata item whose label repeats
// it. Every proposed ISSN is then confirmed by the registry, not by the proposal:
// Crossref must register the same title and publisher under that ISSN, and the ISSN
// Portal record for two confirmed ISSNs must name the same ISSN-L, which Crossref
// must again register under the catalog title. Only identifier fields are read.
//
// Re-running is cheap and convergent: journals whose feeds already carry an
// ISSN-L are skipped.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"sci-rss-list/internal/catalog"
)

const portalChecks = 2

// publisherHosts narrows Crossref journal records for journals whose titles are
// shared by another publisher's journal.
var publisherHosts = map[string]string{
	"ACS":  "American Chemical Society",
	"APS":  "American Physical Society",
	"PNAS": "National Academy of Sciences",
}

type entry struct {
	title      string
	publishers map[string]bool
	feeds      int
}

type result struct {
	title  string
	issnL  string
	reason string
}

// pace spaces out requests to the public registries, which rate limit free clients.
var pace = 700 * time.Millisecond

func main() {
	dataPath := flag.String("data", filepath.Join("data", "feeds.json"), "catalog JSON path")
	apply := flag.Bool("apply", false, "write confirmed ISSN-L values into the catalog")
	requestTimeout := flag.Duration("request-timeout", 45*time.Second, "per-request timeout")
	interval := flag.Duration("interval", pace, "pause between registry requests")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: go run ./tools/issnlookup [flags]\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	pace = *interval

	feeds, _, err := catalog.Load(*dataPath)
	if err != nil {
		die(err)
	}
	shared := propagateIssnL(feeds)
	if shared > 0 {
		fmt.Printf("shared an existing ISSN-L with %d sibling feed(s)\n", shared)
	}
	entries := pendingEntries(feeds)
	if len(entries) == 0 {
		fmt.Println("every multi-feed journal already carries an issn_l")
		if *apply {
			applyIssnL(feeds, nil, *dataPath)
		}
		return
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].title < entries[j].title })
	fmt.Printf("multi-feed journals awaiting an ISSN-L: %d\n\n", len(entries))

	client := &http.Client{Timeout: *requestTimeout}
	var confirmed, unresolved []result
	for _, e := range entries {
		issnL, reason, err := resolve(client, e)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", e.title, err)
		}
		if issnL == "" {
			if reason == "" {
				reason = "lookup failed"
			}
			unresolved = append(unresolved, result{title: e.title, reason: reason})
			fmt.Printf("  -  %-58s %s\n", e.title, reason)
			continue
		}
		confirmed = append(confirmed, result{title: e.title, issnL: issnL})
		fmt.Printf("  ok %-58s %s\n", e.title, issnL)
	}

	fmt.Printf("\nresolved: %d; unresolved: %d\n", len(confirmed), len(unresolved))
	for _, r := range unresolved {
		fmt.Printf("  unresolved %-56s %s\n", r.title, r.reason)
	}
	if !*apply {
		fmt.Println("\nno file written; pass -apply to store confirmed ISSN-L values")
		return
	}
	fmt.Println()
	applyIssnL(feeds, confirmed, *dataPath)
}

// resolve proposes the ISSNs of one journal and confirms a single ISSN-L for them.
func resolve(client *http.Client, e entry) (issnL, reason string, err error) {
	members := memberISSNs(client, e.title)
	if len(members) == 0 {
		return "", "no ISSN found for this title", nil
	}
	host := publisherHint(e.publishers)
	var values []string
	for _, member := range members {
		if len(values) >= portalChecks {
			break
		}
		registered, err := crossrefJournal(client, member)
		if err != nil {
			return "", "", err
		}
		if !sameTitle(registered.title, e.title) {
			continue
		}
		if !publisherMatches(host, registered.publisher) {
			continue
		}
		value, err := portalIssnL(client, member)
		if err != nil {
			return "", "", err
		}
		if value == "" {
			continue
		}
		values = append(values, value)
	}
	distinct := agreedIssnL(values)
	if len(distinct) == 0 {
		return "", "no registry confirmed this title under a proposed ISSN", nil
	}
	if len(distinct) > 1 {
		return "", fmt.Sprintf("the journal's ISSNs map to different ISSN-L values (%s)", strings.Join(distinct, ", ")), nil
	}
	return accept(client, e.title, distinct[0], host)
}

// agreedIssnL returns the ISSN-L values the registry reports for a journal's own
// ISSNs. More than one value means the proposals were not the same journal, and
// nothing is guessed from them.
func agreedIssnL(values []string) []string {
	var out []string
	for _, value := range values {
		if issnPattern.MatchString(value) {
			out = append(out, value)
		}
	}
	return unique(out)
}

// accept re-reads the candidate as a Crossref journal record, so the value stored is
// the ISSN-L itself and not only a linked member ISSN.
func accept(client *http.Client, title, candidate, host string) (issnL, reason string, err error) {
	linked, err := crossrefJournal(client, candidate)
	if err != nil {
		return "", "", err
	}
	if !sameTitle(linked.title, title) {
		return "", fmt.Sprintf("Crossref title %q differs under %s", linked.title, candidate), nil
	}
	if !publisherMatches(host, linked.publisher) {
		return "", fmt.Sprintf("Crossref publisher %q differs under %s", linked.publisher, candidate), nil
	}
	return candidate, "", nil
}

// publisherMatches narrows a journal whose title another publisher also uses. The
// registry words publisher names loosely -- PNAS registers itself under the journal
// title -- so a shared phrase counts as the same publisher.
func publisherMatches(hint, found string) bool {
	if hint == "" {
		return true
	}
	want, got := normalizeTitle(hint), normalizeTitle(found)
	return want == got || strings.Contains(got, want) || strings.Contains(want, got)
}

// memberISSNs proposes ISSNs that may belong to the journal. Crossref is asked for
// works filed under the title first, then Wikidata for an item labelled with it.
func memberISSNs(client *http.Client, title string) []string {
	var out []string
	if found, err := crossrefWorks(client, title); err == nil {
		out = append(out, found...)
	} else {
		fmt.Fprintf(os.Stderr, "%s: Crossref work search: %v\n", title, err)
	}
	if len(out) < portalChecks {
		found, err := wikidataISSNs(client, title)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: Wikidata search: %v\n", title, err)
		}
		out = append(out, found...)
	}
	return unique(out)
}

type crossrefWork struct {
	ContainerTitle []string `json:"container-title"`
	ISSN           []string `json:"ISSN"`
}

// membersFromWorks keeps only the ISSNs of works whose container title repeats the
// catalog title, because a Crossref query also returns the journals it cites.
func membersFromWorks(items []crossrefWork, title string) []string {
	var out []string
	for _, item := range items {
		for _, container := range item.ContainerTitle {
			if sameTitle(container, title) {
				out = append(out, item.ISSN...)
				break
			}
		}
	}
	return unique(out)
}

func crossrefWorks(client *http.Client, title string) ([]string, error) {
	values := url.Values{
		"query.container-title": {title},
		"filter":                {"type:journal-article"},
		"rows":                  {"20"},
		"select":                {"container-title,ISSN"},
	}
	var out struct {
		Message struct {
			Items []crossrefWork `json:"items"`
		} `json:"message"`
	}
	endpoint := "https://api.crossref.org/works?" + values.Encode()
	if err := getJSON(client, endpoint, &out); err != nil {
		return nil, err
	}
	return membersFromWorks(out.Message.Items, title), nil
}

func wikidataISSNs(client *http.Client, title string) ([]string, error) {
	values := url.Values{
		"action":   {"wbsearchentities"},
		"search":   {title},
		"language": {"en"},
		"format":   {"json"},
		"type":     {"item"},
		"limit":    {"7"},
	}
	var found struct {
		Search []struct {
			ID      string `json:"id"`
			Display struct {
				Label struct {
					Value string `json:"value"`
				} `json:"label"`
			} `json:"display"`
		} `json:"search"`
	}
	endpoint := "https://www.wikidata.org/w/api.php?" + values.Encode()
	if err := getJSON(client, endpoint, &found); err != nil {
		return nil, err
	}
	var items []string
	for _, item := range found.Search {
		if sameTitle(item.Display.Label.Value, title) {
			items = append(items, item.ID)
		}
	}
	if len(items) == 0 {
		return nil, nil
	}
	claims := url.Values{
		"action":   {"wbgetclaims"},
		"entity":   {strings.Join(items, "|")},
		"property": {"P236"},
		"format":   {"json"},
	}
	var out struct {
		Claims map[string][]struct {
			Mainsnak struct {
				Datavalue struct {
					Value string `json:"value"`
				} `json:"datavalue"`
			} `json:"mainsnak"`
		} `json:"claims"`
	}
	if err := getJSON(client, "https://www.wikidata.org/w/api.php?"+claims.Encode(), &out); err != nil {
		return nil, err
	}
	var issns []string
	for _, statements := range out.Claims {
		for _, statement := range statements {
			if issnPattern.MatchString(statement.Mainsnak.Datavalue.Value) {
				issns = append(issns, statement.Mainsnak.Datavalue.Value)
			}
		}
	}
	return issns, nil
}

type journalRecord struct {
	title     string
	publisher string
}

func crossrefJournal(client *http.Client, issn string) (journalRecord, error) {
	var out struct {
		Message struct {
			Title     string `json:"title"`
			Publisher string `json:"publisher"`
		} `json:"message"`
	}
	if err := getJSON(client, "https://api.crossref.org/journals/"+issn, &out); err != nil {
		return journalRecord{}, err
	}
	return journalRecord{title: out.Message.Title, publisher: out.Message.Publisher}, nil
}

var (
	issnPattern     = regexp.MustCompile(`^\d{4}-\d{3}[0-9X]$`)
	portalAttribute = regexp.MustCompile(`(?i)issnl="(\d{4}-\d{3}[0-9X])"`)
	portalResource  = regexp.MustCompile(`(?i)/resource/ISSN-L/(\d{4}-\d{3}[0-9X])`)
)

// portalIssnL reads the ISSN-L the ISSN Portal record itself carries. The page marks
// it both as an attribute and as a link to the linking record.
func portalIssnL(client *http.Client, issn string) (string, error) {
	body, err := getText(client, "https://portal.issn.org/resource/ISSN/"+issn)
	if err != nil {
		return "", err
	}
	return issnLFromPortalPage(body), nil
}

func issnLFromPortalPage(body string) string {
	for _, pattern := range []*regexp.Regexp{portalAttribute, portalResource} {
		if match := pattern.FindStringSubmatch(body); match != nil {
			return match[1]
		}
	}
	return ""
}

// propagateIssnL copies a journal's known ISSN-L onto its sibling feeds, so adding
// a second feed for a resolved journal needs no new lookup.
func propagateIssnL(feeds []catalog.Feed) int {
	known := map[string]string{}
	for _, f := range feeds {
		if f.CanonicalJournal != nil && f.IssnL != nil {
			known[strings.TrimSpace(*f.CanonicalJournal)] = *f.IssnL
		}
	}
	changed := 0
	for i := range feeds {
		f := &feeds[i]
		if f.CanonicalJournal == nil || f.IssnL != nil {
			continue
		}
		if issnL, ok := known[strings.TrimSpace(*f.CanonicalJournal)]; ok {
			f.IssnL = catalog.Ptr(issnL)
			changed++
		}
	}
	return changed
}

// pendingEntries returns the journals with more than one feed and no ISSN-L yet.
func pendingEntries(feeds []catalog.Feed) []entry {
	groups := map[string]*entry{}
	resolved := map[string]bool{}
	for _, f := range feeds {
		if f.CanonicalJournal == nil {
			continue
		}
		title := strings.TrimSpace(*f.CanonicalJournal)
		e := groups[title]
		if e == nil {
			e = &entry{title: title, publishers: map[string]bool{}}
			groups[title] = e
		}
		e.feeds++
		e.publishers[f.Publisher] = true
		if f.IssnL != nil {
			resolved[title] = true
		}
	}
	var out []entry
	for _, e := range groups {
		if e.feeds > 1 && !resolved[e.title] {
			out = append(out, *e)
		}
	}
	return out
}

func publisherHint(publishers map[string]bool) string {
	if len(publishers) != 1 {
		return ""
	}
	for publisher := range publishers {
		return publisherHosts[publisher]
	}
	return ""
}

func unique(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range values {
		key := strings.ToUpper(strings.TrimSpace(v))
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, key)
	}
	return out
}

func applyIssnL(feeds []catalog.Feed, confirmed []result, dataPath string) {
	byTitle := map[string]string{}
	for _, r := range confirmed {
		byTitle[r.title] = r.issnL
	}
	changed := 0
	for i := range feeds {
		j := feeds[i].CanonicalJournal
		if j == nil || feeds[i].IssnL != nil {
			continue
		}
		if issnL, ok := byTitle[strings.TrimSpace(*j)]; ok {
			feeds[i].IssnL = catalog.Ptr(issnL)
			changed++
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
		die(fmt.Errorf("%d validation error(s) after applying ISSN-L", len(errs)))
	}
	if err := catalog.Save(dataPath, feeds); err != nil {
		die(err)
	}
	fmt.Printf("ok: set issn_l on %d entries\n", changed)
}

func getJSON(client *http.Client, endpoint string, out any) error {
	body, err := request(client, endpoint, "application/json")
	if err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}

func getText(client *http.Client, endpoint string) (string, error) {
	body, err := request(client, endpoint, "text/html")
	return string(body), err
}

const maxAttempts = 4

// request spaces out calls and retries the throttled public endpoints.
func request(client *http.Client, endpoint, accept string) ([]byte, error) {
	var lastErr error
	for attempt := range maxAttempts {
		if attempt > 0 {
			time.Sleep(time.Duration(1<<attempt) * 4 * time.Second)
		}
		time.Sleep(pace)
		req, err := http.NewRequest(http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "sci-rss-list-catalog-maintenance")
		req.Header.Set("Accept", accept)
		res, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
		res.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		if res.StatusCode == http.StatusTooManyRequests || res.StatusCode >= 500 {
			lastErr = fmt.Errorf("%s returned %s", endpoint, res.Status)
			continue
		}
		if res.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("%s returned %s", endpoint, res.Status)
		}
		return body, nil
	}
	return nil, lastErr
}

func sameTitle(a, b string) bool {
	return normalizeTitle(a) != "" && normalizeTitle(a) == normalizeTitle(b)
}

// normalizeTitle compares titles on letters, digits and spaces only. Crossref and
// Wikidata escape "&" in titles, which would otherwise break an exact comparison.
func normalizeTitle(s string) string {
	s = html.UnescapeString(strings.ToLower(strings.TrimSpace(s)))
	s = strings.ReplaceAll(s, "&", " and ")
	var b strings.Builder
	lastSpace := false
	for _, r := range s {
		switch {
		case r == ' ':
			if !lastSpace && b.Len() > 0 {
				b.WriteRune(' ')
			}
			lastSpace = true
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastSpace = false
		}
	}
	return strings.TrimSpace(b.String())
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

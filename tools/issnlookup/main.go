// Command issnlookup resolves ISSN-L values for the journals that have more than
// one feed in the catalog, so those feeds share one stable identity. Titles it
// cannot confirm stay null; nothing is guessed.
//
// Discovery only proposes ISSNs that may belong to the journal: a Crossref work
// filed under the catalog title, a Wikidata item labelled with it, or an ISSN the
// publisher's own journal page prints. Every proposed ISSN is then confirmed by the
// registry, not by the proposal: Crossref must register the same title and publisher
// under that ISSN, and the ISSN Portal record for two confirmed ISSNs must name the
// same ISSN-L, which Crossref must again register under the catalog title. When the
// confirmed ISSNs fall into more than one portal title record, only a record titled
// as this journal is this journal's: an edition in another language or a superseded
// title is left alone. Only identifier fields are read.
//
// Re-running is cheap and convergent: journals whose feeds already carry an
// ISSN-L are skipped.
package main

import (
	"encoding/json"
	"errors"
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

const scopeSingle = "single_journal"

// publisherHosts narrows Crossref journal records for journals whose titles are
// shared by another publisher's journal.
var publisherHosts = map[string]string{
	"ACS":  "American Chemical Society",
	"APS":  "American Physical Society",
	"PNAS": "National Academy of Sciences",
}

// publisherPrefixes scope a Crossref work search to one publisher's deposits, which
// keeps a parent journal from drowning out the journal actually asked for.
var publisherPrefixes = map[string]string{
	"ACS":                    "10.1021",
	"APS":                    "10.1103",
	"PNAS":                   "10.1073",
	"Cell Press":             "10.1016",
	"Elsevier/ScienceDirect": "10.1016",
}

type entry struct {
	title      string
	titles     []string
	publishers map[string]bool
	feeds      int
	urls       []string
	sources    []string
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
	members := memberISSNs(client, e)
	if len(members) == 0 {
		return "", "no ISSN found for this title", nil
	}
	host := publisherHint(e.publishers)
	var confirmed []portalRecord
	for _, member := range members {
		if len(confirmed) >= portalChecks {
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
		record, err := portalRecordFor(client, member)
		if err != nil {
			return "", "", err
		}
		if record.issnL == "" {
			continue
		}
		confirmed = append(confirmed, record)
	}
	var values []string
	for _, record := range confirmed {
		values = append(values, record.issnL)
	}
	distinct := agreedIssnL(values)
	switch {
	case len(distinct) == 0:
		return "", "no registry confirmed this title under a proposed ISSN", nil
	case len(distinct) > 1:
		// The proposals reached the portal as more than one title record. Only the
		// record that carries the catalog title names this journal's ISSN-L; an other
		// language or superseded edition of the same journal never does.
		var named []string
		for _, record := range confirmed {
			if titleGroupMatches(e, record.title) {
				named = append(named, record.issnL)
			}
		}
		if distinct = agreedIssnL(named); len(distinct) != 1 {
			return "", ambiguousReason(values, confirmed), nil
		}
	}
	return accept(client, e.title, distinct[0], host)
}

// titleGroupMatches asks whether an ISSN Portal record title is the catalog's wording
// for this journal. The labels of the journal's own feeds count as that wording, so a
// record titled by a variant still resolves; an other language edition or a superseded
// title with its own subtitle never does.
func titleGroupMatches(e entry, recordTitle string) bool {
	titles := e.titles
	if len(titles) == 0 {
		titles = []string{e.title}
	}
	recordTitle = strings.TrimSpace(strings.TrimSuffix(
		recordTitle, editionQualifier.FindString(recordTitle)))
	for _, want := range titles {
		if proposalMatches(want, recordTitle) {
			return true
		}
	}
	return false
}

// editionQualifier is the trailing medium a portal record appends to an edition, as
// in "Environmental health perspectives (Online)". It never carries a title of its
// own, so dropping it separates editions of one journal from differently named ones.
var editionQualifier = regexp.MustCompile(`(?i)\s*\([^()]*\)\s*$`)

// titleOf names a journal by the label its own single feed carries, so a title group
// recorded under a variant wording still counts for the catalog title it stands for.
func titleOf(f catalog.Feed) string {
	if f.FeedScope == scopeSingle && f.FeedType == nil {
		return strings.TrimSpace(f.Journal)
	}
	if f.CanonicalJournal != nil {
		return strings.TrimSpace(*f.CanonicalJournal)
	}
	return strings.TrimSpace(f.Journal)
}

// ambiguousReason names the ISSN-L values and the record titles that could not be
// told apart, so an unresolved journal can be checked by a person.
func ambiguousReason(values []string, confirmed []portalRecord) string {
	parts := make([]string, 0, len(confirmed))
	for _, record := range confirmed {
		parts = append(parts, fmt.Sprintf("%s is %q", record.issnL, record.title))
	}
	return fmt.Sprintf("the journal's ISSNs map to different ISSN-L values (%s); %s",
		strings.Join(unique(values), ", "), strings.Join(parts, "; "))
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

// proposalMatches compares the catalog title with a source that only proposes an
// ISSN, where the wording may be a variation of the official title. Confirmation
// under a registry record uses sameTitle alone.
func proposalMatches(want, got string) bool {
	if sameTitle(want, got) {
		return true
	}
	return sameTitle(dropArticle(want), dropArticle(got))
}

func dropArticle(title string) string {
	title = strings.TrimSpace(title)
	for _, article := range []string{"The ", "A ", "An "} {
		if len(title) > len(article) && strings.EqualFold(title[:len(article)], article) {
			return title[len(article):]
		}
	}
	return title
}

func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

// memberISSNs proposes ISSNs that may belong to the journal: Crossref works filed
// under the title, a Wikidata item labelled with it, and the ISSNs the publisher's
// own journal page prints. A proposal is never trusted -- every one of them still
// has to survive the registry check in resolve.
func memberISSNs(client *http.Client, e entry) []string {
	var out []string
	note := func(what string, err error) {
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %s: %v\n", e.title, what, err)
		}
	}
	out = append(out, issnsFromURLs(e.urls)...)
	if len(out) < portalChecks {
		found, err := crossrefWorks(client, e.title, publisherPrefix(e.publishers))
		note("Crossref work search", err)
		out = append(out, found...)
	}
	if len(out) < portalChecks {
		found, err := wikidataISSNs(client, e.title)
		note("Wikidata search", err)
		out = append(out, found...)
	}
	for _, source := range e.sources {
		if len(out) >= portalChecks {
			break
		}
		found, err := pageISSNs(client, source)
		note("official page "+source, err)
		out = append(out, found...)
	}
	return unique(out)
}

// issnsFromURLs reads the ISSNs a feed URL already carries, as the Wiley and
// ScienceDirect feed patterns do. A token that is not this journal's ISSN is weeded
// out by the registry check.
func issnsFromURLs(urls []string) []string {
	var out []string
	for _, raw := range urls {
		for _, match := range urlIssnPattern.FindAllStringSubmatch(raw, portalChecks*2) {
			out = append(out, match[1]+"-"+match[2]+strings.ToUpper(match[3]))
		}
	}
	return unique(out)
}

func publisherPrefix(publishers map[string]bool) string {
	prefix := ""
	for publisher := range publishers {
		next, ok := publisherPrefixes[publisher]
		if !ok || (prefix != "" && prefix != next) {
			return ""
		}
		prefix = next
	}
	return prefix
}

// pageISSNs reads the ISSNs a publisher page prints, which is how a journal whose
// registry deposits sit under a parent title still gets a usable proposal.
func pageISSNs(client *http.Client, rawurl string) ([]string, error) {
	body, err := getText(client, rawurl)
	if err != nil {
		return nil, err
	}
	return unique(pageIssnPattern.FindAllString(body, portalChecks*3)), nil
}

type crossrefWork struct {
	ContainerTitle []string `json:"container-title"`
	ISSN           []string `json:"ISSN"`
}

// membersFromWorks keeps only the ISSNs of works filed under the catalog title,
// because a Crossref query also returns the journals a work cites.
func membersFromWorks(items []crossrefWork, title string) []string {
	var out []string
	for _, item := range items {
		for _, container := range item.ContainerTitle {
			if proposalMatches(title, container) {
				out = append(out, item.ISSN...)
				break
			}
		}
	}
	return unique(out)
}

func crossrefWorks(client *http.Client, title, prefix string) ([]string, error) {
	filter := "type:journal-article"
	if prefix != "" {
		filter += ",prefix:" + prefix
	}
	values := url.Values{
		"query.container-title": {title},
		"filter":                {filter},
		"rows":                  {"25"},
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
		"limit":    {"25"},
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
		if proposalMatches(title, item.Display.Label.Value) {
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
		if missing(err) {
			return journalRecord{}, nil
		}
		return journalRecord{}, err
	}
	return journalRecord{title: out.Message.Title, publisher: out.Message.Publisher}, nil
}

var (
	issnPattern     = regexp.MustCompile(`^\d{4}-\d{3}[0-9X]$`)
	pageIssnPattern = regexp.MustCompile(`\d{4}-\d{3}[0-9X]`)
	urlIssnPattern  = regexp.MustCompile(`(\d{4})-?(\d{3})([0-9xX])`)
	portalAttribute = regexp.MustCompile(`(?i)issnl="(\d{4}-\d{3}[0-9X])"`)
	portalResource  = regexp.MustCompile(`(?i)/resource/ISSN-L/(\d{4}-\d{3}[0-9X])`)
	portalTitleTag  = regexp.MustCompile(`(?is)<title>\s*ISSN \d{4}-\d{3}[0-9X]\s*-\s*(.*?)\s*</title>`)
)

// portalRecord is one ISSN Portal record: the title it catalogues and the ISSN-L that
// groups it with its own editions. Two ISSNs of different titles share no ISSN-L, so
// the record title is what separates a journal from another edition of it.
type portalRecord struct {
	title string
	issnL string
}

// portalRecordFor reads the ISSN Portal record of one ISSN.
func portalRecordFor(client *http.Client, issn string) (portalRecord, error) {
	body, err := getText(client, "https://portal.issn.org/resource/ISSN/"+issn)
	if err != nil {
		if missing(err) {
			return portalRecord{}, nil
		}
		return portalRecord{}, err
	}
	return portalRecordFromPage(body), nil
}

func portalRecordFromPage(body string) portalRecord {
	record := portalRecord{issnL: issnLFromPortalPage(body)}
	if match := portalTitleTag.FindStringSubmatch(body); match != nil {
		record.title = match[1]
	}
	return record
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
		if name := titleOf(f); name != "" && !contains(e.titles, name) {
			e.titles = append(e.titles, name)
		}
		if len(e.urls) < portalChecks*2 && f.URL != "" && !contains(e.urls, f.URL) {
			e.urls = append(e.urls, f.URL)
		}
		if len(e.sources) < 2 && f.Source != "" && !contains(e.sources, f.Source) {
			e.sources = append(e.sources, f.Source)
		}
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

// notFound is an answer rather than a transport failure: the registry has no record
// under that ISSN, so this proposal is unusable and the next one gets a turn.
type notFound struct{ endpoint string }

func (e *notFound) Error() string { return e.endpoint + " returned 404 Not Found" }

func missing(err error) bool {
	var nf *notFound
	return errors.As(err, &nf)
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
		if res.StatusCode == http.StatusNotFound {
			return nil, &notFound{endpoint: endpoint}
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

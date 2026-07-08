package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"sci-rss-list/internal/catalog"
)

func main() {
	dataPath := flag.String("data", filepath.Join("data", "feeds.json"), "catalog JSON path")
	dryRun := flag.Bool("dry-run", false, "check input without writing")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: go run ./tools/addfeeds [flags] new-feeds.json\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}

	existing, _, err := catalog.Load(*dataPath)
	if err != nil {
		die(err)
	}
	additions, err := readInput(flag.Arg(0))
	if err != nil {
		die(err)
	}
	if len(additions) == 0 {
		die(fmt.Errorf("no feeds to add"))
	}
	if dup := duplicateURL(existing, additions); dup != "" {
		die(fmt.Errorf("duplicate url: %s", dup))
	}

	combined := append(existing, additions...)
	raw, err := rawShape(combined)
	if err != nil {
		die(err)
	}
	if errs := catalog.ValidateShape(combined, raw); len(errs) > 0 {
		for _, err := range errs {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
	if *dryRun {
		fmt.Printf("ok: %d feed(s) can be added\n", len(additions))
		return
	}
	if err := catalog.Save(*dataPath, combined); err != nil {
		die(err)
	}
	fmt.Printf("ok: added %d feed(s) to %s\n", len(additions), *dataPath)
}

func readInput(path string) ([]catalog.Feed, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var feeds []catalog.Feed
	if err := json.Unmarshal(b, &feeds); err == nil {
		return feeds, nil
	}
	var feed catalog.Feed
	if err := json.Unmarshal(b, &feed); err != nil {
		return nil, err
	}
	return []catalog.Feed{feed}, nil
}

func duplicateURL(existing, additions []catalog.Feed) string {
	seen := map[string]string{}
	for _, f := range existing {
		seen[catalog.CanonicalURL(f.URL)] = f.URL
	}
	for _, f := range additions {
		url := catalog.CanonicalURL(f.URL)
		if seen[url] != "" {
			return f.URL
		}
		seen[url] = f.URL
	}
	return ""
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

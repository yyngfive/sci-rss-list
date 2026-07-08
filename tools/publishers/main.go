package main

import (
	"fmt"
	"os"
	"path/filepath"

	"sci-rss-list/internal/catalog"
)

func main() {
	root, err := os.Getwd()
	if err != nil {
		die(err)
	}
	feeds, _, err := catalog.Load(filepath.Join(root, "data", "feeds.json"))
	if err != nil {
		die(err)
	}
	if err := catalog.WritePublisherMarkdown(filepath.Join(root, "publishers"), feeds); err != nil {
		die(err)
	}
	if err := catalog.WriteReadmePublisherIndex(filepath.Join(root, "README.md"), feeds); err != nil {
		die(err)
	}
	fmt.Printf("ok: wrote %d publisher pages\n", catalog.PublisherCount(feeds))
}

func die(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

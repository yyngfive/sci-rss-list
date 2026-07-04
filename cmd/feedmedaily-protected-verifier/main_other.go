//go:build !windows

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "FeedMeDaily protected-feed verifier is only supported on Windows")
	os.Exit(1)
}

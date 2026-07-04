package main

import (
	"flag"
	"fmt"
	"strings"
)

type cliOptions struct {
	VerificationID   string
	JobID            string
	VerificationHost string
	CallbackURL      string
	UserDataDir      string
	LogsDir          string
	AppVersion       string
	FeedURLs         []string
}

type repeatedStrings []string

func (r *repeatedStrings) String() string {
	return strings.Join(*r, ",")
}

func (r *repeatedStrings) Set(value string) error {
	clean := strings.TrimSpace(value)
	if clean != "" {
		*r = append(*r, clean)
	}
	return nil
}

func parseOptions(args []string) (cliOptions, error) {
	var opts cliOptions
	var feeds repeatedStrings
	fs := flag.NewFlagSet("feedmedaily-protected-verifier", flag.ContinueOnError)
	fs.StringVar(&opts.VerificationID, "verification-id", "", "verification request id")
	fs.StringVar(&opts.JobID, "job-id", "", "sync job id")
	fs.StringVar(&opts.VerificationHost, "verification-host", "", "protected feed host")
	fs.StringVar(&opts.CallbackURL, "callback-url", "", "FeedMeDaily verification callback URL")
	fs.StringVar(&opts.UserDataDir, "user-data-dir", "", "persistent WebView2 profile directory")
	fs.StringVar(&opts.LogsDir, "logs-dir", "", "FeedMeDaily logs directory")
	fs.StringVar(&opts.AppVersion, "app-version", "", "FeedMeDaily app version")
	fs.Var(&feeds, "feed-url", "protected feed URL; may be repeated")
	if err := fs.Parse(args); err != nil {
		return cliOptions{}, err
	}
	opts.VerificationID = strings.TrimSpace(opts.VerificationID)
	opts.JobID = strings.TrimSpace(opts.JobID)
	opts.VerificationHost = strings.TrimSpace(opts.VerificationHost)
	opts.CallbackURL = strings.TrimSpace(opts.CallbackURL)
	opts.UserDataDir = strings.TrimSpace(opts.UserDataDir)
	opts.LogsDir = strings.TrimSpace(opts.LogsDir)
	opts.AppVersion = strings.TrimSpace(opts.AppVersion)
	opts.FeedURLs = uniqueStrings(feeds)
	if opts.VerificationID == "" || opts.VerificationHost == "" || opts.CallbackURL == "" || opts.UserDataDir == "" || len(opts.FeedURLs) == 0 {
		return cliOptions{}, fmt.Errorf("verification-id, verification-host, callback-url, user-data-dir, and at least one feed-url are required")
	}
	return opts, nil
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		clean := strings.TrimSpace(value)
		if clean == "" {
			continue
		}
		key := strings.ToLower(clean)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, clean)
	}
	return result
}

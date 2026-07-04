package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var callbackHTTPClient = &http.Client{Timeout: 15 * time.Second}

type callbackPayload struct {
	VerificationID   string         `json:"verification_id"`
	VerificationHost string         `json:"verification_host"`
	FeedURL          string         `json:"feed_url"`
	Status           string         `json:"status"`
	ContentType      string         `json:"content_type"`
	FeedXML          string         `json:"feed_xml"`
	Error            string         `json:"error"`
	SessionVerified  bool           `json:"session_verified"`
	CapturedFeeds    []capturedFeed `json:"captured_feeds"`
}

type capturedFeed struct {
	FeedURL     string `json:"feed_url"`
	ContentType string `json:"content_type"`
	FeedXML     string `json:"feed_xml"`
}

func postPayload(callbackURL string, payload callbackPayload) (int, string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, "", err
	}
	request, err := http.NewRequest(http.MethodPost, callbackURL, bytes.NewReader(body))
	if err != nil {
		return 0, "", err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := callbackHTTPClient.Do(request)
	if err != nil {
		return 0, "", err
	}
	defer response.Body.Close()
	return response.StatusCode, response.Status, nil
}

func orderedCapturedFeeds(items map[string]capturedFeed) []capturedFeed {
	result := make([]capturedFeed, 0, len(items))
	for _, item := range items {
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i].FeedURL) < strings.ToLower(result[j].FeedURL)
	})
	return result
}

func looksLikeXML(contentType string, body string) bool {
	if strings.Contains(strings.ToLower(contentType), "xml") {
		return true
	}
	trimmed := strings.TrimLeft(body, "\ufeff \t\r\n")
	for _, prefix := range []string{"<?xml", "<rdf:RDF", "<rss", "<feed"} {
		if strings.HasPrefix(strings.ToLower(trimmed), strings.ToLower(prefix)) {
			return true
		}
	}
	return false
}

func looksLikeChallenge(contentType string, body string) bool {
	if !strings.Contains(strings.ToLower(contentType), "html") {
		return false
	}
	sample := strings.ToLower(body)
	return strings.Contains(sample, "just a moment") ||
		strings.Contains(sample, "enable javascript and cookies") ||
		strings.Contains(sample, "cf-browser-verification") ||
		strings.Contains(sample, "__cf_chl_") ||
		strings.Contains(sample, "challenge-platform")
}

func buildLogPath(logsDir string) string {
	clean := strings.TrimSpace(logsDir)
	if clean == "" {
		return ""
	}
	return filepath.Join(clean, "protected-verifier", time.Now().Format("2006-01-02")+".log")
}

type verifierLogger struct {
	verificationID string
	path           string
}

func newVerifierLogger(opts cliOptions) verifierLogger {
	return verifierLogger{verificationID: opts.VerificationID, path: buildLogPath(opts.LogsDir)}
}

func (l verifierLogger) Printf(format string, args ...any) {
	if strings.TrimSpace(l.path) == "" {
		return
	}
	line := fmt.Sprintf("%s %s %s\r\n", time.Now().Format("2006-01-02 15:04:05.000"), l.verificationID, fmt.Sprintf(format, args...))
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return
	}
	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = file.WriteString(line)
}

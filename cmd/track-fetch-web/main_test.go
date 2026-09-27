package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type failedOutput struct{}

func (failedOutput) Write([]byte) (int, error) { return 0, errors.New("simulated broken pipe") }

func TestSnapshotModeWritesRawResponseAndManifestOnly(t *testing.T) {
	const source = "https://requested.example/articles/one"
	const final = "https://final.example/redirected/one"
	const paragraph = "Snapshot mode preserves response body bytes separately from extracted readable Markdown. The derived file contains only the selected page content, not a source header, title summary, or retrieval banner."
	raw := []byte(`<html><head><title>Snapshot Fixture</title><meta property="article:published_time" content="2026-07-02"><meta property="article:modified_time" content="2026-07-03T10:11:12+09:00"></head><body><article><h1>Snapshot Fixture</h1><p>` + paragraph + `</p></article></body></html>`)
	finalURL, err := url.Parse(final)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		finalRequest := req.Clone(req.Context())
		finalRequest.URL = finalURL
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
			Body:       io.NopCloser(strings.NewReader(string(raw))),
			Request:    finalRequest,
		}, nil
	})}
	dir := filepath.Join(t.TempDir(), "snapshot")
	var stdout, stderr strings.Builder
	start := time.Now().UTC()
	code := runWithHTTPClient([]string{"--snapshot-dir", dir, source}, &stdout, &stderr, client)
	end := time.Now().UTC()
	if code != 0 {
		t.Fatalf("run code=%d stderr=%s", code, stderr.String())
	}
	if calls != 1 {
		t.Fatalf("HTTP acquisitions = %d, want exactly one", calls)
	}
	if strings.Count(stdout.String(), "\n") != 1 {
		t.Fatalf("stdout must contain exactly one manifest line: %q", stdout.String())
	}
	var manifest map[string]json.RawMessage
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout.String())), &manifest); err != nil {
		t.Fatalf("stdout is not manifest JSON only: %v\n%s", err, stdout.String())
	}
	wantKeys := []string{"schema_version", "source_url", "final_url", "retrieved_at", "original_path", "text_path", "original_sha256", "text_sha256", "published", "modified", "extraction_method"}
	if len(manifest) != len(wantKeys) {
		t.Fatalf("manifest keys = %v", manifest)
	}
	for _, key := range wantKeys {
		if _, ok := manifest[key]; !ok {
			t.Errorf("manifest missing %q: %v", key, manifest)
		}
	}
	var values struct {
		SchemaVersion  int    `json:"schema_version"`
		SourceURL      string `json:"source_url"`
		FinalURL       string `json:"final_url"`
		RetrievedAt    string `json:"retrieved_at"`
		OriginalPath   string `json:"original_path"`
		TextPath       string `json:"text_path"`
		OriginalSHA256 string `json:"original_sha256"`
		TextSHA256     string `json:"text_sha256"`
		Published      struct {
			Raw       string  `json:"raw"`
			Precision string  `json:"precision"`
			Timestamp *string `json:"timestamp"`
		} `json:"published"`
		Modified struct {
			Raw       string  `json:"raw"`
			Precision string  `json:"precision"`
			Timestamp *string `json:"timestamp"`
		} `json:"modified"`
		ExtractionMethod string `json:"extraction_method"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout.String())), &values); err != nil {
		t.Fatal(err)
	}
	if values.SchemaVersion != 1 || values.SourceURL != source || values.FinalURL != final || values.ExtractionMethod != "readability-v1" {
		t.Fatalf("manifest identity = %+v", values)
	}
	retrievedAt, err := time.Parse(time.RFC3339Nano, values.RetrievedAt)
	if err != nil {
		t.Fatalf("retrieved_at is not RFC3339: %q: %v", values.RetrievedAt, err)
	}
	if retrievedAt.Before(start) || retrievedAt.After(end) {
		t.Fatalf("retrieved_at %s is outside acquisition interval [%s, %s]", retrievedAt, start, end)
	}
	if values.Published.Raw != "2026-07-02" || values.Published.Precision != "date" || values.Published.Timestamp != nil {
		t.Fatalf("published = %+v", values.Published)
	}
	if values.Modified.Raw != "2026-07-03T10:11:12+09:00" || values.Modified.Precision != "instant" || values.Modified.Timestamp == nil {
		t.Fatalf("modified = %+v", values.Modified)
	}
	for name, relative := range map[string]string{"original": values.OriginalPath, "text": values.TextPath} {
		within, err := filepath.Rel(dir, relative)
		if err != nil || !filepath.IsAbs(relative) || filepath.Clean(relative) != relative || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) {
			t.Fatalf("%s path is not a secured path inside the snapshot: %q (%v)", name, relative, err)
		}
	}
	if filepath.Base(values.OriginalPath) != "original.html" || filepath.Base(values.TextPath) != "text.md" {
		t.Fatalf("unexpected snapshot paths: %q, %q", values.OriginalPath, values.TextPath)
	}
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("snapshot directory mode = %v, %v", info, err)
	}
	savedOriginal, err := os.ReadFile(values.OriginalPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(savedOriginal) != string(raw) {
		t.Fatal("saved original differs from the single response body")
	}
	if info, err := os.Stat(values.OriginalPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("original file mode = %v, %v", info, err)
	}
	savedText, err := os.ReadFile(values.TextPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(savedText) != paragraph || strings.Contains(string(savedText), "Source]") || strings.Contains(string(savedText), "Clipped") {
		t.Fatalf("text.md is not extraction-only content: %q", savedText)
	}
	if info, err := os.Stat(values.TextPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("text file mode = %v, %v", info, err)
	}
	originalHash := sha256.Sum256(raw)
	textHash := sha256.Sum256(savedText)
	if values.OriginalSHA256 != hex.EncodeToString(originalHash[:]) || values.TextSHA256 != hex.EncodeToString(textHash[:]) {
		t.Fatalf("hashes do not match saved bytes: %+v", values)
	}
}

func TestSnapshotModeDoesNotOverwriteAndRequiresURL(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(`<html><body><article><p>Enough content for an acquisition fixture.</p></article></body></html>`)), Request: req}, nil
	})}
	dir := filepath.Join(t.TempDir(), "snapshots")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(dir, "keep.txt")
	if err := os.WriteFile(sentinel, []byte("do not replace"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := make([]string, 0, 2)
	for range 2 {
		var stdout, stderr strings.Builder
		if code := runWithHTTPClient([]string{"--snapshot-dir", dir, "https://example.test"}, &stdout, &stderr, client); code != 0 {
			t.Fatalf("snapshot in an existing container failed: %s", stderr.String())
		}
		var manifest struct {
			OriginalPath string `json:"original_path"`
			TextPath     string `json:"text_path"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(stdout.String())), &manifest); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, manifest.OriginalPath, manifest.TextPath)
	}
	if paths[0] == paths[2] || paths[1] == paths[3] {
		t.Fatalf("successive snapshots reused output paths: %v", paths)
	}
	if got, err := os.ReadFile(sentinel); err != nil || string(got) != "do not replace" {
		t.Fatalf("existing file changed: %q, %v", got, err)
	}

	local := filepath.Join(t.TempDir(), "page.html")
	if err := os.WriteFile(local, []byte("<html></html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	var localOut, localErr strings.Builder
	if code := runWithHTTPClient([]string{"--snapshot-dir", filepath.Join(t.TempDir(), "not-created"), local}, &localOut, &localErr, client); code != 2 {
		t.Fatalf("local file snapshot code=%d stderr=%s", code, localErr.String())
	}
	if localOut.Len() != 0 {
		t.Fatalf("invalid snapshot source wrote stdout: %q", localOut.String())
	}
}

func TestSnapshotRejectsOversizedResponseWithoutFilesOrManifest(t *testing.T) {
	const source = "https://example.test/oversized"
	body := strings.NewReader(strings.Repeat("x", maxResponseBytes+1))
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(body), Request: req}, nil
	})}
	dir := filepath.Join(t.TempDir(), "oversized")
	var stdout, stderr strings.Builder
	if code := runWithHTTPClient([]string{"--snapshot-dir", dir, source}, &stdout, &stderr, client); code == 0 {
		t.Fatal("oversized response unexpectedly succeeded")
	}
	if stdout.Len() != 0 {
		t.Fatalf("oversized response wrote a manifest: %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "exceeds") {
		t.Fatalf("oversize error not reported: %s", stderr.String())
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("oversized response created snapshot files: %v", err)
	}
}

func TestSnapshotManifestWriteFailureRemovesPartialSnapshot(t *testing.T) {
	const source = "https://example.test/article"
	body := `<html><body><article><p>` + strings.Repeat("Readable content to produce a snapshot. ", 8) + `</p></article></body></html>`
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	dir := filepath.Join(t.TempDir(), "snapshot")
	var stderr strings.Builder
	if code := runWithHTTPClient([]string{"--snapshot-dir", dir, source}, failedOutput{}, &stderr, client); code == 0 {
		t.Fatal("snapshot succeeded despite a manifest stdout failure")
	}
	if !strings.Contains(stderr.String(), "simulated broken pipe") {
		t.Fatalf("stdout failure was not reported: %s", stderr.String())
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("incomplete snapshot remains after output failure: entries=%v err=%v", entries, err)
	}
}

func TestEventTimeUsesRetrievalWhenOnlyModifiedMetadataExists(t *testing.T) {
	const source = "https://example.test/article"
	body := `<html><body><article><p>` + strings.Repeat("Readable article content. ", 8) + `</p></article></body></html>`
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/html"}, "Last-Modified": []string{"Thu, 01 Jan 2026 00:00:00 GMT"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	start := time.Now().UTC()
	var stdout, stderr strings.Builder
	if code := runWithHTTPClient([]string{source}, &stdout, &stderr, client); code != 0 {
		t.Fatalf("run code=%d stderr=%s", code, stderr.String())
	}
	var record struct {
		Time        string `json:"time"`
		URL         string `json:"url"`
		RetrievedAt string `json:"retrieved_at"`
		Published   struct {
			Raw       string  `json:"raw"`
			Precision string  `json:"precision"`
			Timestamp *string `json:"timestamp"`
		} `json:"published"`
		Modified struct {
			Raw       string  `json:"raw"`
			Precision string  `json:"precision"`
			Timestamp *string `json:"timestamp"`
		} `json:"modified"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout.String())), &record); err != nil {
		t.Fatal(err)
	}
	got, err := time.Parse(time.RFC3339Nano, record.Time)
	if err != nil {
		t.Fatalf("event time is not RFC3339: %q", record.Time)
	}
	if got.Before(start.Truncate(time.Second)) || got.After(time.Now().UTC().Truncate(time.Second)) || record.URL != source {
		t.Fatalf("event time/source = %s / %q; modified time must not be publication time", got, record.URL)
	}
	if record.Published.Raw != "" || record.Published.Precision != "absent" || record.Published.Timestamp != nil {
		t.Fatalf("published metadata was inferred from Last-Modified: %+v", record.Published)
	}
	if record.Modified.Raw != "Thu, 01 Jan 2026 00:00:00 GMT" || record.Modified.Precision != "instant" || record.Modified.Timestamp == nil || *record.Modified.Timestamp != "2026-01-01T00:00:00Z" {
		t.Fatalf("Last-Modified was not retained separately: %+v", record.Modified)
	}
	if _, err := time.Parse(time.RFC3339Nano, record.RetrievedAt); err != nil {
		t.Fatalf("retrieved_at is not RFC3339: %q", record.RetrievedAt)
	}
}

package main

import (
	"bytes"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/dataset"
)

// fixture is a local article-shaped HTML page with og metadata, exercised through the file-input
// path so the CLI tests never touch the network.
const fixture = "testdata/article.html"

func TestRunFileJSONL(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{fixture}, &stdout, &stderr); code != 0 {
		t.Fatalf("run = %d, stderr: %s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
	line := strings.TrimSuffix(stdout.String(), "\n")
	if strings.Contains(line, "\n") {
		t.Fatalf("expected exactly one JSONL record, got:\n%s", stdout.String())
	}
	var rec dataset.Record
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		t.Fatalf("record is not valid JSON: %v\n%s", err, line)
	}
	if err := dataset.Validate(dataset.KindEvent, rec); err != nil {
		t.Fatalf("record does not validate: %v", err)
	}
	if rec["version"] != float64(dataset.SchemaVersion) {
		t.Errorf("version = %v, want %v", rec["version"], dataset.SchemaVersion)
	}
	// time comes from the page's declared publication time, as RFC 3339.
	if rec["time"] != "2026-06-01T08:00:00Z" {
		t.Errorf("time = %v", rec["time"])
	}
	if _, err := time.Parse(time.RFC3339, rec["time"].(string)); err != nil {
		t.Errorf("time is not RFC 3339: %v", err)
	}
	if rec["title"] != "Prototype Article" {
		t.Errorf("title = %v", rec["title"])
	}
	if _, ok := rec["url"]; ok {
		t.Errorf("file input should not carry a url: %v", rec)
	}
	if rec["image"] != "https://example.com/lead.jpg" {
		t.Errorf("image = %v", rec["image"])
	}
	md, _ := rec["markdown"].(string)
	if !strings.Contains(md, "related note") {
		t.Errorf("markdown missing content:\n%s", md)
	}
	if strings.Contains(md, "Home") || strings.Contains(md, "Privacy") {
		t.Errorf("markdown kept pruned chrome:\n%s", md)
	}
	// The h1 repeats the title, so it must be dropped from the body.
	if strings.Contains(md, "# Prototype Article") {
		t.Errorf("markdown kept the title heading:\n%s", md)
	}
}

func TestRunNote(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--note", fixture}, &stdout, &stderr); code != 0 {
		t.Fatalf("run = %d, stderr: %s", code, stderr.String())
	}
	body := stdout.String()
	if !strings.HasPrefix(body, "Clipped ") {
		t.Errorf("note should start with a provenance line, got:\n%s", body)
	}
	if !strings.Contains(body, "![](https://example.com/lead.jpg)") {
		t.Errorf("note should carry the lead image:\n%s", body)
	}
	if !strings.Contains(body, "related note") {
		t.Errorf("note should carry the content:\n%s", body)
	}
}

func TestRunOut(t *testing.T) {
	var stdout, stderr bytes.Buffer
	out := filepath.Join(t.TempDir(), "clip.jsonl")
	if code := run([]string{fixture, "--out", out}, &stdout, &stderr); code != 0 {
		t.Fatalf("run = %d, stderr: %s", code, stderr.String())
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(data), "\n") || strings.Count(string(data), "\n") != 1 {
		t.Fatalf("file should hold exactly one JSONL record:\n%s", data)
	}
	var summary map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &summary); err != nil {
		t.Fatalf("summary is not valid JSON: %v\n%s", err, stdout.String())
	}
	if summary["path"] != out || summary["kind"] != string(dataset.KindEvent) ||
		summary["records"] != float64(1) || summary["title"] != "Prototype Article" {
		t.Errorf("summary = %v", summary)
	}
}

func TestRunUsage(t *testing.T) {
	for _, args := range [][]string{nil, {fixture, fixture}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 {
			t.Errorf("run(%v) = %d, want 2", args, code)
		}
		if !strings.Contains(stderr.String(), "exactly one page URL") {
			t.Errorf("run(%v) stderr = %q", args, stderr.String())
		}
	}
}

func TestRunMissingFile(t *testing.T) {
	var stdout, stderr bytes.Buffer
	path := filepath.Join(t.TempDir(), "nope.html")
	if code := run([]string{path}, &stdout, &stderr); code != 1 {
		t.Fatalf("run = %d, want 1; stderr: %s", code, stderr.String())
	}
}

// A page with no title and no URL cannot form a conformant event record; the tool must refuse
// rather than emit one.
func TestRunRefusesTitlelessClip(t *testing.T) {
	html := `<html><body><p>Some plain prose that is long enough to be picked as the content of this page, with no title and no heading anywhere.</p></body></html>`
	path := filepath.Join(t.TempDir(), "titleless.html")
	if err := os.WriteFile(path, []byte(html), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{path}, &stdout, &stderr); code != 1 {
		t.Fatalf("run = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), `missing required field "title"`) {
		t.Errorf("stderr = %q", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout should stay clean: %q", stdout.String())
	}
}

// The SSRF guard must refuse non-public addresses before any connection is made.
func TestRunRefusesPrivateAddress(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"http://127.0.0.1:1/"}, &stdout, &stderr); code != 1 {
		t.Fatalf("run = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "refusing to fetch non-public address") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestSourceURLFor(t *testing.T) {
	if got := sourceURLFor(nil); got != "" {
		t.Errorf("sourceURLFor(nil) = %q, want \"\"", got)
	}
	u, _ := url.Parse("https://example.com/a")
	if got := sourceURLFor(u); got != "https://example.com/a" {
		t.Errorf("sourceURLFor = %q", got)
	}
	// A redirect hop changes the resolved URL; the record must point at the final one.
	final, _ := url.Parse("https://example.com/landed")
	if got := sourceURLFor(final); got != "https://example.com/landed" {
		t.Errorf("sourceURLFor redirect = %q", got)
	}
}

func TestReorderArgs(t *testing.T) {
	tests := []struct{ in, want []string }{
		{[]string{"page.html"}, []string{"page.html"}},
		{[]string{"--note", "page.html"}, []string{"--note", "page.html"}},
		{[]string{"page.html", "--note"}, []string{"--note", "page.html"}},
		{[]string{"page.html", "--out", "clip.jsonl"}, []string{"--out", "clip.jsonl", "page.html"}},
		{[]string{"--url", "https://e.com/a", "--timeout", "5s"}, []string{"--url", "https://e.com/a", "--timeout", "5s"}},
		{[]string{"page.html", "--out=clip.jsonl", "--note"}, []string{"--out=clip.jsonl", "--note", "page.html"}},
		{[]string{"page.html", "--", "--weird"}, []string{"page.html", "--weird"}},
	}
	for _, tt := range tests {
		if got := reorderArgs(tt.in); !slices.Equal(got, tt.want) {
			t.Errorf("reorderArgs(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

package site

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/note"
)

// writeNote lays down a note file and its sidecar metadata directly, the minimum ParseFile needs.
func writeNote(t *testing.T, cfg *config.Config, id int64, title, body string) {
	t.Helper()
	if err := os.MkdirAll(cfg.NoteDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.NotePath(id), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cfg.MetadataDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	meta := note.Metadata{Version: note.CurrentMetadataVersion, Title: title}
	if err := note.WriteMetadata(cfg.MetadataPath(id), meta); err != nil {
		t.Fatal(err)
	}
}

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	vault := t.TempDir()
	t.Setenv("TRACK_VAULT", vault)
	t.Setenv("TRACK_CACHE_DIR", filepath.Join(vault, ".cache"))
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	return cfg
}

func TestBuildRendersRootAndLinks(t *testing.T) {
	cfg := testConfig(t)
	writeNote(t, cfg, 100, "Home", "# Home\n\nsee [[Child]] and [[Outsider]]\n")
	writeNote(t, cfg, 200, "Child", "# Child\n\nhello\n")
	writeNote(t, cfg, 300, "Outsider", "# Outsider\n\nnot published\n")

	resolve := func(key string) (int64, bool) {
		switch key {
		case "Home":
			return 100, true
		case "Child":
			return 200, true
		case "Outsider":
			return 300, true
		}
		return 0, false
	}

	out := t.TempDir()
	res, err := Build(cfg, resolve, Options{Root: 100, IDs: []int64{200}}, out)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if res.OutDir != out {
		t.Fatalf("OutDir = %q, want %q", res.OutDir, out)
	}

	index := readFile(t, filepath.Join(out, "index.html"))
	// In-set link points to the child page; out-of-set link is inert plain text.
	if !strings.Contains(index, `href="200.html"`) {
		t.Fatalf("index missing in-set link to child:\n%s", index)
	}
	if strings.Contains(index, "300.html") || strings.Contains(index, "Outsider.html") {
		t.Fatalf("out-of-set note must not be linked:\n%s", index)
	}
	if !strings.Contains(index, "Outsider") {
		t.Fatalf("out-of-set link text should remain as inert text:\n%s", index)
	}
	if strings.Contains(index, "[[") {
		t.Fatalf("wiki syntax leaked into html:\n%s", index)
	}

	// Child page exists and links home.
	child := readFile(t, filepath.Join(out, "200.html"))
	if !strings.Contains(child, `href="index.html"`) {
		t.Fatalf("child page missing home nav:\n%s", child)
	}
	if !fileExists(filepath.Join(out, "style.css")) {
		t.Fatalf("style.css not written")
	}
	// Outsider was not selected, so no page for it.
	if fileExists(filepath.Join(out, "300.html")) {
		t.Fatalf("unselected note should not produce a page")
	}
}

func TestBuildRendersGFMTable(t *testing.T) {
	cfg := testConfig(t)
	writeNote(t, cfg, 100, "Home", "# Home\n\n| a | b |\n| --- | --- |\n| 1 | 2 |\n")
	out := t.TempDir()
	if _, err := Build(cfg, func(string) (int64, bool) { return 0, false }, Options{Root: 100}, out); err != nil {
		t.Fatalf("build: %v", err)
	}
	index := readFile(t, filepath.Join(out, "index.html"))
	if !strings.Contains(index, "<table>") || !strings.Contains(index, "<td>1</td>") {
		t.Fatalf("GFM table not rendered:\n%s", index)
	}
}

func TestBuildRequiresRoot(t *testing.T) {
	cfg := testConfig(t)
	if _, err := Build(cfg, nil, Options{}, t.TempDir()); err == nil {
		t.Fatalf("expected error when root is missing")
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

package site

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

// fakeFrontend creates a minimal static-mode frontend build to copy into the site.
func fakeFrontend(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	index := `<!doctype html><script>var t="__TRACK_DEFAULT_THEME__"</script>__TRACK_COLOR_OVERRIDES__<div id=root></div>`
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("console.log(1)"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func vaultStore(t *testing.T) (*config.Config, *store.Store) {
	t.Helper()
	vault := t.TempDir()
	cfg := &config.Config{
		VaultDir:          vault,
		DBPath:            filepath.Join(vault, ".track", "index.db"),
		Extensions:        []string{".md"},
		DateFormat:        "2006-01-02",
		JournalDateFormat: "20060102",
	}
	s, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return cfg, s
}

func writeVaultNote(t *testing.T, cfg *config.Config, id int64, title, body string) {
	t.Helper()
	path := cfg.NotePath(id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := note.WriteMetadata(cfg.MetadataPath(id), note.Metadata{Version: note.CurrentMetadataVersion, Title: title}); err != nil {
		t.Fatal(err)
	}
}

func readJSON[T any](t *testing.T, path string) T {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return v
}

func TestBuildVaultBundle(t *testing.T) {
	cfg, s := vaultStore(t)
	writeVaultNote(t, cfg, 100, "Home", "# Home\n\ngo to [[Child]] and [[Outsider]]\n")
	writeVaultNote(t, cfg, 200, "Child", "# Child\n\nback [[Home]]\n")
	writeVaultNote(t, cfg, 300, "Outsider", "# Outsider\n")
	if _, err := index.New(cfg, s).Full(); err != nil {
		t.Fatalf("index: %v", err)
	}

	out := t.TempDir()
	res, err := Build(cfg, s, Options{Root: 100, IDs: []int64{200}}, fakeFrontend(t), out)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(res.Notes) != 2 {
		t.Fatalf("expected 2 published notes, got %v", res.Notes)
	}

	// Frontend copied in, with server-only placeholders substituted.
	if !fileExists(filepath.Join(out, "index.html")) || !fileExists(filepath.Join(out, "assets", "app.js")) {
		t.Fatalf("frontend not copied into site")
	}
	indexHTML, _ := os.ReadFile(filepath.Join(out, "index.html"))
	if strings.Contains(string(indexHTML), "__TRACK_") {
		t.Fatalf("index.html still has unsubstituted placeholders:\n%s", indexHTML)
	}

	// notes.json holds the published set only.
	notes := readJSON[struct {
		Notes []jsonSearchResult `json:"notes"`
	}](t, filepath.Join(out, "data", "notes.json"))
	if len(notes.Notes) != 2 {
		t.Fatalf("notes.json should list 2 notes, got %d", len(notes.Notes))
	}

	// Root note: body keeps wiki links for the frontend; out-of-set note not in graph.
	root := readJSON[jsonNoteResponse](t, filepath.Join(out, "data", "note", "100.json"))
	if !strings.Contains(root.Note.Body, "[[Child]]") {
		t.Fatalf("root body should keep wiki links: %q", root.Note.Body)
	}

	// Child note has a backlink from Home.
	child := readJSON[jsonNoteResponse](t, filepath.Join(out, "data", "note", "200.json"))
	if len(child.Backlinks) != 1 || child.Backlinks[0].NoteID != 100 {
		t.Fatalf("child should have a backlink from 100, got %v", child.Backlinks)
	}

	// graph.json: edge 100->200 present; nothing references 300 (out of set).
	graph := readJSON[struct {
		Graph jsonGraph `json:"graph"`
	}](t, filepath.Join(out, "data", "graph.json"))
	if !hasEdge(graph.Graph.Edges, 100, 200) {
		t.Fatalf("graph missing edge 100->200: %+v", graph.Graph.Edges)
	}
	for _, n := range graph.Graph.Nodes {
		if n.NoteID == 300 {
			t.Fatalf("out-of-set note 300 should not be a graph node")
		}
	}

	// resolve.json maps titles to published notes only.
	resolve := readJSON[map[string]jsonRef](t, filepath.Join(out, "data", "resolve.json"))
	if resolve["Child"].NoteID != 200 {
		t.Fatalf("resolve[Child] should be 200, got %v", resolve["Child"])
	}
	if _, ok := resolve["Outsider"]; ok {
		t.Fatalf("out-of-set note should not be resolvable")
	}

	// site.json names the entry note.
	site := readJSON[jsonSite](t, filepath.Join(out, "data", "site.json"))
	if site.Root != 100 || site.Title != "Home" {
		t.Fatalf("unexpected site.json: %+v", site)
	}
}

func TestBuildRequiresRoot(t *testing.T) {
	cfg, s := vaultStore(t)
	if _, err := Build(cfg, s, Options{}, fakeFrontend(t), t.TempDir()); err == nil {
		t.Fatalf("expected error when root is missing")
	}
}

func hasEdge(edges []jsonGraphEdge, src, dst int64) bool {
	for _, e := range edges {
		if e.SourceID == src && e.TargetID == dst {
			return true
		}
	}
	return false
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

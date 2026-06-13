package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

func TestAPIHandlers(t *testing.T) {
	cfg := &config.Config{
		VaultDir:          t.TempDir(),
		DBPath:            filepath.Join(t.TempDir(), "index.db"),
		Extensions:        []string{".md"},
		DateFormat:        "2006-01-02",
		JournalDateFormat: "20060102",
	}
	if err := os.MkdirAll(cfg.NoteDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.NotePath(100), []byte("# Alpha\n\nSee [[Beta]].\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.NotePath(200), []byte("# Beta\n\nBody.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.UpsertNote(&note.Note{ID: 100, Mtime: 200, Meta: note.Metadata{Title: "Alpha", Tags: []string{"project"}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertNote(&note.Note{ID: 200, Mtime: 100, Meta: note.Metadata{Title: "Beta", Tags: []string{note.GeneratedByAITag}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceLinks(100, []int64{200}); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(New(cfg, s).Handler())
	t.Cleanup(server.Close)

	search := getJSON(t, server.URL+"/api/search?q=Alpha")
	results := search["results"].([]any)
	if len(results) != 1 || results[0].(map[string]any)["title"] != "Alpha" {
		t.Fatalf("unexpected search results: %v", results)
	}
	tags := results[0].(map[string]any)["tags"].([]any)
	if len(tags) != 1 || tags[0] != "project" {
		t.Fatalf("unexpected search result tags: %v", results[0])
	}

	resolved := getJSON(t, server.URL+"/api/resolve?term=Beta")
	if resolved["found"] != true || resolved["note"].(map[string]any)["note_id"].(float64) != 200 {
		t.Fatalf("unexpected resolve result: %v", resolved)
	}

	noteResp := getJSON(t, server.URL+"/api/note?id=200")
	noteBody := noteResp["note"].(map[string]any)
	noteTags := noteBody["tags"].([]any)
	if noteBody["generated_by_ai"] != true || noteBody["title"] != "Beta" || len(noteTags) != 1 || noteTags[0] != note.GeneratedByAITag {
		t.Fatalf("unexpected note response: %v", noteBody)
	}

	graph := getJSON(t, server.URL+"/api/graph/local?id=100")["graph"].(map[string]any)
	if len(graph["nodes"].([]any)) != 2 || len(graph["edges"].([]any)) != 1 {
		t.Fatalf("unexpected graph: %v", graph)
	}

	full := getJSON(t, server.URL+"/api/graph")["graph"].(map[string]any)
	if len(full["nodes"].([]any)) != 2 || len(full["edges"].([]any)) != 1 {
		t.Fatalf("unexpected full graph: %v", full)
	}
	if full["center_id"].(float64) != 0 {
		t.Fatalf("full graph should have no center, got %v", full["center_id"])
	}
	firstNode := full["nodes"].([]any)[0].(map[string]any)
	if firstNode["path"] == nil || firstNode["path"] == "" {
		t.Fatalf("full graph node should carry a resolved path: %v", firstNode)
	}
}

func getJSON(t *testing.T, url string) map[string]any {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get %s status = %d", url, resp.StatusCode)
	}
	var decoded map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
	return decoded
}

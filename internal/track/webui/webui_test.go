package webui

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	now := time.Now().Unix()
	if err := s.UpsertNote(&note.Note{ID: 100, Mtime: now, Meta: note.Metadata{Title: "Alpha", Tags: []string{"project"}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertNote(&note.Note{ID: 200, Mtime: now - 86400, Meta: note.Metadata{Title: "Beta", Tags: []string{note.GeneratedByAITag}}}); err != nil {
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
	if cp, _ := noteBody["copy_path"].(string); cp == "" {
		t.Fatalf("note response should carry a copy_path: %v", noteBody)
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

	activity := getJSON(t, server.URL+"/api/activity?days=7")["activity"].(map[string]any)
	if activity["days"].(float64) != 7 || activity["total"].(float64) != 2 {
		t.Fatalf("unexpected activity response: %v", activity)
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

// putNoteSetup builds a server with one indexed note (id, title) whose markdown and sidecar exist on
// disk, ready for save/conflict tests.
func putNoteSetup(t *testing.T, id int64, title, body string) (*httptest.Server, *config.Config) {
	t.Helper()
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
	if err := os.WriteFile(cfg.NotePath(id), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := note.WriteMetadata(cfg.MetadataPath(id), note.Metadata{Title: title}); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.UpsertNote(&note.Note{ID: id, Path: cfg.NotePath(id), Meta: note.Metadata{Title: title}}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(cfg, s).Handler())
	t.Cleanup(server.Close)
	return server, cfg
}

func putNote(t *testing.T, url, jsonBody string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, url, strings.NewReader(jsonBody))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("put %s: %v", url, err)
	}
	defer resp.Body.Close()
	var decoded map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&decoded)
	return resp.StatusCode, decoded
}

func TestPutNoteSavesBodyWithEtag(t *testing.T) {
	server, cfg := putNoteSetup(t, 100, "Alpha", "old body\n")

	got := getJSON(t, server.URL+"/api/note?id=100")["note"].(map[string]any)
	etag, _ := got["etag"].(string)
	if etag == "" {
		t.Fatalf("GET should return an etag, got %v", got)
	}

	body := `{"body":"new body line\n","etag":"` + etag + `"}`
	code, resp := putNote(t, server.URL+"/api/note?id=100", body)
	if code != http.StatusOK || resp["saved"] != true {
		t.Fatalf("put status=%d resp=%v", code, resp)
	}
	if resp["etag"] == etag {
		t.Fatalf("etag should change after save")
	}
	raw, err := os.ReadFile(cfg.NotePath(100))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "new body line\n" {
		t.Fatalf("file body = %q, want %q", string(raw), "new body line\n")
	}
}

func TestPutNoteRejectsStaleEtag(t *testing.T) {
	server, cfg := putNoteSetup(t, 100, "Alpha", "old body\n")

	code, resp := putNote(t, server.URL+"/api/note?id=100", `{"body":"x\n","etag":"deadbeef"}`)
	if code != http.StatusConflict {
		t.Fatalf("stale etag should be 409, got %d (%v)", code, resp)
	}
	raw, _ := os.ReadFile(cfg.NotePath(100))
	if string(raw) != "old body\n" {
		t.Fatalf("file must be unchanged on conflict, got %q", string(raw))
	}
}

func TestPutNoteRequiresEtag(t *testing.T) {
	server, _ := putNoteSetup(t, 100, "Alpha", "old body\n")
	code, _ := putNote(t, server.URL+"/api/note?id=100", `{"body":"x\n"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("missing etag should be 400, got %d", code)
	}
}

func TestIndexInjectsConfiguredTheme(t *testing.T) {
	cfg := &config.Config{
		VaultDir:   t.TempDir(),
		DBPath:     filepath.Join(t.TempDir(), "index.db"),
		Extensions: []string{".md"},
		WebTheme:   "dark",
	}
	s, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	server := httptest.NewServer(New(cfg, s).Handler())
	t.Cleanup(server.Close)

	resp, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(resp.Body)
	html := string(bodyBytes)
	if !strings.Contains(html, `var serverDefault = "dark"`) {
		t.Fatalf("served HTML should inject the dark default theme")
	}
	if strings.Contains(html, "__TRACK_DEFAULT_THEME__") {
		t.Fatalf("placeholder should be replaced")
	}
	if !strings.Contains(html, `id="activity-grid"`) {
		t.Fatalf("served HTML should include the sidebar activity grid")
	}
}

func TestIndexInjectsPaletteOverrides(t *testing.T) {
	palettePath := filepath.Join(t.TempDir(), "colors.yml")
	if err := os.WriteFile(palettePath, []byte("dark:\n  accent: \"#123456\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		VaultDir:      t.TempDir(),
		DBPath:        filepath.Join(t.TempDir(), "index.db"),
		Extensions:    []string{".md"},
		WebColorsPath: palettePath,
	}
	s, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	server := httptest.NewServer(New(cfg, s).Handler())
	t.Cleanup(server.Close)

	resp, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	html := string(body)
	if !strings.Contains(html, `id="track-colors"`) || !strings.Contains(html, "--accent:#123456;") {
		t.Fatalf("served HTML should inject palette overrides, got:\n%s", html)
	}
}

func TestIndexNoPaletteRemovesPlaceholder(t *testing.T) {
	cfg := &config.Config{VaultDir: t.TempDir(), DBPath: filepath.Join(t.TempDir(), "index.db"), Extensions: []string{".md"}}
	s, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	server := httptest.NewServer(New(cfg, s).Handler())
	t.Cleanup(server.Close)

	resp, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	html := string(body)
	if strings.Contains(html, "__TRACK_COLOR_OVERRIDES__") {
		t.Fatalf("color placeholder should be replaced even with no palette")
	}
	if strings.Contains(html, `id="track-colors"`) {
		t.Fatalf("no palette should mean no override style block")
	}
}

package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

func TestReadAndWatcherContinuePastInvalidFiles(t *testing.T) {
	cfg := &config.Config{
		VaultDir: t.TempDir(), DBPath: filepath.Join(t.TempDir(), "index.db"),
		Extensions: []string{".md"}, DateFormat: "2006-01-02",
	}
	if err := os.MkdirAll(cfg.NoteDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	addIndexedTestNote(t, cfg, st, 100, "Original")
	if err := os.WriteFile(filepath.Join(cfg.NoteDir(), "100 (conflicted copy).md"), []byte("conflict"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.NotePath(200), []byte("bad metadata"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.MetadataPath(200), []byte("version: 1\ntitle: [\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.NotePath(300), []byte("valid after the conflict"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := note.WriteMetadata(cfg.MetadataPath(300), note.Metadata{Title: "Healthy"}); err != nil {
		t.Fatal(err)
	}
	server := New(cfg, st)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/search?q=Healthy", nil))
	var payload struct {
		Results []store.SearchResult `json:"results"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || len(payload.Results) != 1 || payload.Results[0].NoteID != 300 {
		t.Fatalf("read refresh lost valid files: %d: %s", response.Code, response.Body.String())
	}
	// Drive the same reconcile used by fsnotify; it must update valid files and broadcast SSE.
	if err := os.WriteFile(cfg.NotePath(400), []byte("new valid note"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := note.WriteMetadata(cfg.MetadataPath(400), note.Metadata{Title: "Later"}); err != nil {
		t.Fatal(err)
	}
	ch := server.events.subscribe()
	defer server.events.unsubscribe(ch)
	server.reconcileAfterChange()
	select {
	case ev := <-ch:
		if ev.name != "change" {
			t.Fatalf("unexpected event: %+v", ev)
		}
	default:
		t.Fatal("watcher failed to broadcast successful partial rebuild")
	}
	rows, err := st.AllNotes()
	if err != nil || len(rows) != 3 || rows[2].NoteID != 400 {
		t.Fatalf("watcher lost valid files: %+v, %v", rows, err)
	}
	raw, err := os.ReadFile(cfg.NotePath(100))
	if err != nil || !strings.Contains(string(raw), "Original") {
		t.Fatalf("conflict copy replaced the original: %q, %v", raw, err)
	}
}

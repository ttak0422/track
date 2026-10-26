package webui

import (
	"bytes"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

func TestNoteAPIsRecoverFromCacheRebuildWhileOpen(t *testing.T) {
	t.Setenv("TRACK_CONFIG", filepath.Join(t.TempDir(), "missing.yml"))
	t.Setenv("TRACK_VAULT", t.TempDir())
	t.Setenv("TRACK_CACHE_DIR", t.TempDir())
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.JournalOff = true
	if err := os.MkdirAll(cfg.NoteDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	bodies := map[int64]string{100: "Parent body", 200: "Child body\nup:: [[Parent]]\n"}
	for id, body := range bodies {
		if err := os.WriteFile(cfg.NotePath(id), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		title := "Parent"
		if id == 200 {
			title = "Child"
		}
		if err := note.WriteMetadata(cfg.MetadataPath(id), note.Metadata{Title: title}); err != nil {
			t.Fatal(err)
		}
	}
	// Even an unusable old cache must not stop a fixed app from indexing the
	// authoritative files. Leave it untouched for any older process still using it.
	legacyPath := filepath.Join(filepath.Dir(cfg.DBPath), "index.db")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	legacyBytes := []byte("broken legacy cache")
	if err := os.WriteFile(legacyPath, legacyBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	live, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	s := New(cfg, live)
	httpServer := httptest.NewServer(s.Handler())
	defer httpServer.Close()
	check := func() {
		t.Helper()
		response := getJSON(t, httpServer.URL+"/api/note?id=200")
		got := response["note"].(map[string]any)
		if got["body"] != strings.TrimSpace(bodies[200]) || got["title"] != "Child" {
			t.Fatalf("note unreadable: %v", response)
		}
		trail := response["trail"].([]any)
		if len(trail) != 1 || trail[0].(map[string]any)["title"] != "Parent" {
			t.Fatalf("hierarchy unreadable: %v", response)
		}
		if hits := getJSON(t, httpServer.URL+"/api/search?q=Child")["results"].([]any); len(hits) != 1 {
			t.Fatalf("search results: %v", hits)
		}
		if newest := getJSON(t, httpServer.URL+"/api/notes?sort=created&limit=1")["notes"].([]any); len(newest) != 1 || newest[0].(map[string]any)["title"] != "Child" {
			t.Fatalf("new notes results: %v", newest)
		}
	}
	check() // cold-start rebuild, with a broken old cache still present
	for i := 0; i < 3; i++ {
		if err := store.Reset(cfg.DBPath); err != nil {
			t.Fatal(err)
		}
		// Alternate completed external reindexes with an interrupted reindex:
		// the latter must self-heal on the next normal freshness scan.
		if i%2 == 0 {
			external, err := store.Open(cfg.DBPath)
			if err != nil {
				t.Fatal(err)
			}
			_, rebuildErr := index.New(cfg, external).Full()
			closeErr := external.Close()
			if rebuildErr != nil || closeErr != nil {
				t.Fatalf("external rebuild: %v / %v", rebuildErr, closeErr)
			}
		} else {
			s.active.reindexMu.Lock()
			s.active.lastStale = time.Time{}
			s.active.reindexMu.Unlock()
		}
		check()
	}
	if got, err := os.ReadFile(legacyPath); err != nil || !bytes.Equal(got, legacyBytes) {
		t.Fatalf("legacy cache was changed: %q / %v", got, err)
	}
	for id, body := range bodies {
		if got, err := os.ReadFile(cfg.NotePath(id)); err != nil || string(got) != body {
			t.Fatalf("authoritative body changed: %q / %v", got, err)
		}
	}
}

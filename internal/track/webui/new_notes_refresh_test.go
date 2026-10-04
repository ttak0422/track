package webui

import (
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

// Both frontend refresh triggers must observe committed index data: the watched change event and
// the activity poll that discovers an external write even when the filesystem event was missed.
func TestNewNotesFreshAfterExternalCreation(t *testing.T) {
	for _, source := range []string{"watch", "activity read"} {
		t.Run(source, func(t *testing.T) {
			cfg := &config.Config{
				VaultDir: t.TempDir(), DBPath: filepath.Join(t.TempDir(), "index.db"),
				Extensions: []string{".md"}, DateFormat: "2006-01-02", JournalDateFormat: "20060102",
			}
			st, err := store.Open(cfg.DBPath)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { st.Close() })
			server := New(cfg, st)
			httpServer := httptest.NewServer(server.Handler())
			t.Cleanup(httpServer.Close)
			ch := server.events.subscribe()
			t.Cleanup(func() { server.events.unsubscribe(ch) })
			if source == "watch" {
				server.startWatch()
			} else if err := os.MkdirAll(cfg.NoteDir(), 0o755); err != nil {
				t.Fatal(err)
			}

			// Write files like another process, without touching the server's index. A short burst
			// must become one creation-ordered page, even though every file has the same activity day.
			for _, id := range []int64{100, 200, 300} {
				if err := note.WriteMetadata(cfg.MetadataPath(id), note.Metadata{
					Title: fmt.Sprintf("External %d", id), Created: time.Now().Format("2006-01-02"),
				}); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(cfg.NotePath(id), []byte("External body\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if source == "activity read" {
				activity := getJSON(t, httpServer.URL+"/api/notes")["notes"].([]any)
				if len(activity) != 3 {
					t.Fatalf("activity read missed external creations: %v", activity)
				}
			}
			select {
			case ev := <-ch:
				if ev.name != "change" {
					t.Fatalf("event = %q, want change", ev.name)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("external creation never emitted a change")
			}
			// Check the store before another HTTP read can repair it: emission must follow indexing.
			indexed, err := st.NewestRefs(2)
			if err != nil || len(indexed) != 2 || indexed[0].NoteID != 300 || indexed[1].NoteID != 200 {
				t.Fatalf("change preceded indexed creation list: %v, %v", indexed, err)
			}
			page := getJSON(t, httpServer.URL+"/api/notes?sort=created&limit=2")["notes"].([]any)
			if len(page) != 2 || page[0].(map[string]any)["title"] != "External 300" || page[1].(map[string]any)["title"] != "External 200" {
				t.Fatalf("New lost its creation order or limit: %v", page)
			}
		})
	}
}

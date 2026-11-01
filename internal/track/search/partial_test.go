package search

import (
	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
	"os"
	"path/filepath"
	"testing"
)

func TestPartialBodySearchKeepsHealthyHits(t *testing.T) {
	vault := t.TempDir()
	cfg := &config.Config{VaultDir: vault, DBPath: filepath.Join(vault, "cache.db"), Extensions: []string{".md"}}
	if err := os.MkdirAll(cfg.NoteDir(), 0755); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{1, 2, 3} {
		if err := os.WriteFile(cfg.NotePath(id), []byte("xy needle"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := note.WriteMetadata(cfg.MetadataPath(id), note.Metadata{Title: "Synthetic"}); err != nil {
			t.Fatal(err)
		}
	}
	s, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := index.New(cfg, s).Full(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(cfg.NotePath(2), 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(cfg.NotePath(2), 0644)
	if _, err := os.ReadFile(cfg.NotePath(2)); err == nil {
		t.Skip("permission fixture not enforced")
	}
	results, err := Scoped(cfg, s, "xy", 10, store.SearchBody)
	if err != nil || len(results) != 2 {
		t.Fatalf("healthy matches lost: %+v %v", results, err)
	}
	for _, hit := range results {
		if hit.NoteID == 2 {
			t.Fatal("unverified body returned as a fresh match")
		}
	}
	state, err := s.ReadState()
	if err != nil || !state.Partial || !state.CacheUsed {
		t.Fatalf("missing status: %+v %v", state, err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		changed, err := index.New(cfg, s).RefreshIfStale()
		if err != nil || changed {
			t.Fatalf("unchanged query failure bypassed retry backoff: %v %v", changed, err)
		}
	}
	results, err = Scoped(cfg, s, "needle", 10, store.SearchBody)
	if err != nil || len(results) != 3 {
		t.Fatalf("cached FTS not usable: %+v %v", results, err)
	}
	if err := os.Chmod(cfg.NotePath(2), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := index.New(cfg, s).Full(); err != nil {
		t.Fatal(err)
	}
	state, err = s.ReadState()
	if err != nil || state.Partial {
		t.Fatalf("recovery status %+v %v", state, err)
	}
}

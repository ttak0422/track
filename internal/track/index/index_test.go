package index

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/store"
)

func setup(t *testing.T) (*config.Config, *store.Store) {
	t.Helper()
	vault := t.TempDir()
	cfg := &config.Config{
		VaultDir:   vault,
		DBPath:     filepath.Join(vault, ".track", "index.db"),
		Extensions: []string{".md"},
		DateFormat: "2006-01-02",
		Footmatter: config.FootmatterMarkers{Open: "<!--track", Close: "-->"},
	}
	s, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return cfg, s
}

func writeNote(t *testing.T, cfg *config.Config, id int64, body, footmatter string) {
	t.Helper()
	path := cfg.NotePath(id)
	content := body + "\n\n<!--track\n" + footmatter + "\n-->\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write note %d: %v", id, err)
	}
}

func TestFullIndexesAndLinks(t *testing.T) {
	cfg, s := setup(t)
	// Note 1 is titled "リンク". Note 2's body references リンク → link 2->1.
	writeNote(t, cfg, 1, "# リンク\n\nthe target note", "title: リンク\naliases:\n    - link")
	writeNote(t, cfg, 2, "本文で リンク を参照する", "title: ノート2")

	ix := New(cfg, s)
	rep, err := ix.Full()
	if err != nil {
		t.Fatalf("full: %v", err)
	}
	if rep.Indexed != 2 {
		t.Fatalf("indexed = %d, want 2", rep.Indexed)
	}
	if rep.Links != 1 {
		t.Fatalf("links = %d, want 1", rep.Links)
	}

	back, err := s.Backlinks(1)
	if err != nil {
		t.Fatalf("backlinks: %v", err)
	}
	if len(back) != 1 || back[0].NoteID != 2 {
		t.Fatalf("expected note 2 to backlink note 1, got %+v", back)
	}
}

func TestFullReconcilesDeletions(t *testing.T) {
	cfg, s := setup(t)
	writeNote(t, cfg, 1, "a", "title: A")
	writeNote(t, cfg, 2, "b", "title: B")
	ix := New(cfg, s)
	if _, err := ix.Full(); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(cfg.NotePath(2)); err != nil {
		t.Fatal(err)
	}
	rep, err := ix.Full()
	if err != nil {
		t.Fatal(err)
	}
	if rep.Deleted != 1 {
		t.Fatalf("deleted = %d, want 1", rep.Deleted)
	}
	notes, _ := s.AllNotes()
	if len(notes) != 1 || notes[0].NoteID != 1 {
		t.Fatalf("expected only note 1 to remain, got %+v", notes)
	}
}

func TestOneUpdatesOutgoingLinks(t *testing.T) {
	cfg, s := setup(t)
	writeNote(t, cfg, 1, "# Go", "title: Go")
	writeNote(t, cfg, 2, "placeholder", "title: Two")
	ix := New(cfg, s)
	if _, err := ix.Full(); err != nil {
		t.Fatal(err)
	}

	// Rewrite note 2 to reference Go, then index just that file.
	writeNote(t, cfg, 2, "now mentions Go here", "title: Two")
	if err := ix.One(cfg.NotePath(2)); err != nil {
		t.Fatalf("one: %v", err)
	}
	back, _ := s.Backlinks(1)
	if len(back) != 1 || back[0].NoteID != 2 {
		t.Fatalf("expected 2->1 link after One, got %+v", back)
	}
}

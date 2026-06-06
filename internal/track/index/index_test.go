package index

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/note"
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
	}
	s, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return cfg, s
}

func writeNote(t *testing.T, cfg *config.Config, id int64, body string, meta note.Metadata) {
	t.Helper()
	path := cfg.NotePath(id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create note dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body+"\n"), 0o644); err != nil {
		t.Fatalf("write note %d: %v", id, err)
	}
	if err := note.WriteMetadata(cfg.MetadataPath(id), meta); err != nil {
		t.Fatalf("write metadata %d: %v", id, err)
	}
}

func TestFullIndexesAndLinks(t *testing.T) {
	cfg, s := setup(t)
	// Note 1 is titled "リンク".
	// Note 2's body references リンク → link 2->1.
	writeNote(t, cfg, 1, "# リンク\n\nthe target note", note.Metadata{Title: "リンク", Aliases: []string{"link"}})
	writeNote(t, cfg, 2, "本文で [[リンク]] を参照する", note.Metadata{Title: "ノート2"})

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

func TestFullIndexesOnlyNoteAndJournalDirs(t *testing.T) {
	cfg, s := setup(t)
	writeNote(t, cfg, 1, "# Note", note.Metadata{Title: "Note"})
	if err := os.MkdirAll(cfg.JournalDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.JournalPath("20260606"), []byte("# Journal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := note.WriteMetadata(cfg.MetadataPath(20260606), note.Metadata{Title: "Journal"}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cfg.TemplateDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.TemplatePath(2), []byte("# Template\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.VaultDir, "3.md"), []byte("# Root\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(cfg.NoteDir(), "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.NoteDir(), "nested", "4.md"), []byte("# Nested\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rep, err := New(cfg, s).Full()
	if err != nil {
		t.Fatalf("full: %v", err)
	}
	if rep.Indexed != 2 {
		t.Fatalf("indexed = %d, want note and journal only", rep.Indexed)
	}
	notes, err := s.AllNotes()
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 2 || notes[0].FileKind != config.KindNote || notes[1].FileKind != config.KindJournal {
		t.Fatalf("unexpected indexed files: %+v", notes)
	}
}

func TestFullReconcilesDeletions(t *testing.T) {
	cfg, s := setup(t)
	writeNote(t, cfg, 1, "a", note.Metadata{Title: "A"})
	writeNote(t, cfg, 2, "b", note.Metadata{Title: "B"})
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
	if _, err := os.Stat(cfg.MetadataPath(2)); !os.IsNotExist(err) {
		t.Fatalf("metadata for deleted note should be removed, stat err=%v", err)
	}
	notes, _ := s.AllNotes()
	if len(notes) != 1 || notes[0].NoteID != 1 {
		t.Fatalf("expected only note 1 to remain, got %+v", notes)
	}
}

func TestOneUpdatesOutgoingLinks(t *testing.T) {
	cfg, s := setup(t)
	writeNote(t, cfg, 1, "# Go", note.Metadata{Title: "Go"})
	writeNote(t, cfg, 2, "placeholder", note.Metadata{Title: "Two"})
	ix := New(cfg, s)
	if _, err := ix.Full(); err != nil {
		t.Fatal(err)
	}

	// Rewrite note 2 to reference Go, then index just that file.
	writeNote(t, cfg, 2, "now mentions [[Go]] here", note.Metadata{Title: "Two"})
	if err := ix.One(cfg.NotePath(2)); err != nil {
		t.Fatalf("one: %v", err)
	}
	back, _ := s.Backlinks(1)
	if len(back) != 1 || back[0].NoteID != 2 {
		t.Fatalf("expected 2->1 link after One, got %+v", back)
	}
}

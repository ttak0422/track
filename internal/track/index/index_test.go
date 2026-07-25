package index

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

func setup(t *testing.T) (*config.Config, *store.Store) {
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
	writeNote(t, cfg, 1, "# リンク\n\nthe target note", note.Metadata{Title: "リンク"})
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
	// Reconciling only removes index rows: the sidecar stays on disk as an orphan (doctor reports
	// it), and only explicit commands (track rm, doctor --fix) move files.
	if _, err := os.Stat(cfg.MetadataPath(2)); err != nil {
		t.Fatalf("sidecar of deleted note must stay in place: %v", err)
	}
	if trashed, err := filepath.Glob(filepath.Join(cfg.TrashDir(), "*")); err != nil || len(trashed) != 0 {
		t.Fatalf("a read-path reconcile must not move files into trash, glob=%v err=%v", trashed, err)
	}
	notes, _ := s.AllNotes()
	if len(notes) != 1 || notes[0].NoteID != 1 {
		t.Fatalf("expected only note 1 to remain, got %+v", notes)
	}
}

func TestFullRefusesEmptyScanWithPopulatedIndex(t *testing.T) {
	cfg, s := setup(t)
	ids := []int64{1, 2, 3, 4, 5}
	for _, id := range ids {
		writeNote(t, cfg, id, "body", note.Metadata{Title: fmt.Sprintf("Note %d", id)})
	}
	ix := New(cfg, s)
	if _, err := ix.Full(); err != nil {
		t.Fatal(err)
	}

	// An unmounted or unreadable vault scans as empty; that must refuse, not wipe the index.
	if err := os.RemoveAll(cfg.NoteDir()); err != nil {
		t.Fatal(err)
	}
	if _, err := ix.Full(); err == nil {
		t.Fatal("Full on an empty scan with a populated index should refuse")
	}
	if _, err := ix.RefreshIfStale(); err == nil {
		t.Fatal("RefreshIfStale should propagate the refusal")
	}
	for _, id := range ids {
		if _, err := os.Stat(cfg.MetadataPath(id)); err != nil {
			t.Fatalf("sidecar %d must survive the refused reconcile: %v", id, err)
		}
	}
	notes, err := s.AllNotes()
	if err != nil || len(notes) != len(ids) {
		t.Fatalf("index rows must survive the refused reconcile, got %+v err=%v", notes, err)
	}
}

func TestFullReconcilesEmptyScanBelowGuardFloor(t *testing.T) {
	// `track rm` of a vault's last file leaves a zero scan over a small DB; that must reconcile, not
	// strand phantom rows behind the guard. Uses exactly floor-1 notes to pin the floor from below.
	cfg, s := setup(t)
	for id := int64(1); id <= emptyScanGuardMin-1; id++ {
		writeNote(t, cfg, id, "body", note.Metadata{Title: fmt.Sprintf("Note %d", id)})
	}
	ix := New(cfg, s)
	if _, err := ix.Full(); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(cfg.NoteDir()); err != nil {
		t.Fatal(err)
	}
	rep, err := ix.Full()
	if err != nil {
		t.Fatalf("full below guard floor: %v", err)
	}
	if rep.Deleted != emptyScanGuardMin-1 {
		t.Fatalf("deleted = %d, want %d", rep.Deleted, emptyScanGuardMin-1)
	}
	notes, _ := s.AllNotes()
	if len(notes) != 0 {
		t.Fatalf("expected empty index, got %+v", notes)
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

func TestRefreshIfStale(t *testing.T) {
	cfg, s := setup(t)
	writeNote(t, cfg, 1000, "first", note.Metadata{Title: "First"})
	ix := New(cfg, s)
	if _, err := ix.Full(); err != nil {
		t.Fatalf("full: %v", err)
	}

	// In sync: no reindex, no work.
	if changed, err := ix.RefreshIfStale(); err != nil || changed {
		t.Fatalf("RefreshIfStale on unchanged vault = %v, %v; want false, nil", changed, err)
	}

	// A note written by another process (present on disk, not yet indexed) is picked up.
	writeNote(t, cfg, 2000, "本文 [[First]]", note.Metadata{Title: "Second"})
	if changed, err := ix.RefreshIfStale(); err != nil || !changed {
		t.Fatalf("RefreshIfStale after add = %v, %v; want true, nil", changed, err)
	}
	if _, found, err := s.ResolveTerm("Second"); err != nil || !found {
		t.Fatalf("added note not indexed: found=%v err=%v", found, err)
	}

	// An external edit (new mtime) triggers a reindex.
	path := cfg.NotePath(1000)
	if err := os.WriteFile(path, []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	if changed, err := ix.RefreshIfStale(); err != nil || !changed {
		t.Fatalf("RefreshIfStale after edit = %v, %v; want true, nil", changed, err)
	}

	// A deletion is reconciled.
	if err := os.Remove(cfg.NotePath(2000)); err != nil {
		t.Fatal(err)
	}
	if changed, err := ix.RefreshIfStale(); err != nil || !changed {
		t.Fatalf("RefreshIfStale after delete = %v, %v; want true, nil", changed, err)
	}
	if _, found, _ := s.ResolveTerm("Second"); found {
		t.Fatalf("deleted note still indexed")
	}
}

func TestOneEnsuresDayJournal(t *testing.T) {
	cfg, s := setup(t)
	ix := New(cfg, s)
	writeNote(t, cfg, 1, "body", note.Metadata{Title: "Work"})
	if err := ix.One(cfg.NotePath(1)); err != nil {
		t.Fatalf("one: %v", err)
	}

	// Indexing a note ensures that day's journal exists and is itself indexed (without recursing).
	today := time.Now().Format(cfg.JournalDateFormat)
	if _, err := os.Stat(cfg.JournalPath(today)); err != nil {
		t.Fatalf("today's journal should be auto-created: %v", err)
	}
	ref, found, err := s.ResolveTerm(today)
	if err != nil || !found {
		t.Fatalf("auto-created journal should be indexed: found=%v err=%v", found, err)
	}
	if ref.FileKind != "journal" {
		t.Fatalf("auto-created note should be a journal, got %q", ref.FileKind)
	}

	// The journal is excluded from note_days, so it never appears as activity.
	notes, err := s.NotesOnDay(time.Now().Format(cfg.DateFormat))
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range notes {
		if n.FileKind == "journal" {
			t.Fatalf("journal must not appear in note_days: %+v", n)
		}
	}
}

func TestActivityDaysRecorded(t *testing.T) {
	cfg, s := setup(t)
	ix := New(cfg, s)

	// The CLI-mutation path (One) stamps today's activity day into the sidecar.
	writeNote(t, cfg, 1, "body", note.Metadata{Title: "One"})
	if err := ix.One(cfg.NotePath(1)); err != nil {
		t.Fatalf("one: %v", err)
	}
	today := time.Now().Format(cfg.DateFormat)
	meta, _, err := note.ReadMetadata(cfg.MetadataPath(1))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(meta.Days, today) {
		t.Fatalf("One should record today %q, got %v", today, meta.Days)
	}

	// The editor/external-edit path (RefreshIfStale) stamps the file's mtime day.
	path := cfg.NotePath(1)
	if err := os.WriteFile(path, []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	editDay := time.Date(2026, 6, 19, 12, 0, 0, 0, time.Local)
	if err := os.Chtimes(path, editDay, editDay); err != nil {
		t.Fatal(err)
	}
	if changed, err := ix.RefreshIfStale(); err != nil || !changed {
		t.Fatalf("RefreshIfStale after edit = %v, %v; want true, nil", changed, err)
	}
	want := editDay.Format(cfg.DateFormat)
	meta, _, err = note.ReadMetadata(cfg.MetadataPath(1))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(meta.Days, want) {
		t.Fatalf("RefreshIfStale should record edit day %q, got %v", want, meta.Days)
	}

	// The index exposes the note on that day for agenda lookups.
	notes, err := s.NotesOnDay(want)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || notes[0].NoteID != 1 {
		t.Fatalf("NotesOnDay(%q) = %+v, want note 1", want, notes)
	}
}

func TestRefreshIfStaleDetectsSidecarOnlyChange(t *testing.T) {
	cfg, s := setup(t)
	writeNote(t, cfg, 1, "body", note.Metadata{Title: "One"})
	ix := New(cfg, s)
	if _, err := ix.Full(); err != nil {
		t.Fatal(err)
	}
	if stale, err := ix.RefreshIfStale(); err != nil || stale {
		t.Fatalf("fresh index should not refresh, stale=%v err=%v", stale, err)
	}

	// A sidecar-only edit (synced tag change) never moves the body mtime; it must still be detected.
	if err := note.WriteMetadata(cfg.MetadataPath(1), note.Metadata{Title: "One", Tags: []string{"synced"}}); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(cfg.MetadataPath(1), future, future); err != nil {
		t.Fatal(err)
	}
	stale, err := ix.RefreshIfStale()
	if err != nil || !stale {
		t.Fatalf("sidecar change should trigger a refresh, stale=%v err=%v", stale, err)
	}
	refs, err := s.SearchRefs()
	if err != nil || len(refs) != 1 || len(refs[0].Tags) != 1 || refs[0].Tags[0] != "synced" {
		t.Fatalf("refreshed index should carry the synced tag, got %+v err=%v", refs, err)
	}

	// An orphan sidecar (its note file gone; reads never move it) must not re-trigger refreshes.
	if err := note.WriteMetadata(cfg.MetadataPath(99), note.Metadata{Title: "Orphan"}); err != nil {
		t.Fatal(err)
	}
	if stale, err := ix.RefreshIfStale(); err != nil || stale {
		t.Fatalf("orphan sidecar must not trigger a refresh, stale=%v err=%v", stale, err)
	}
}

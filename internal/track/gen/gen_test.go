package gen

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ttak0422/track/internal/track/config"
)

func testConfig(t *testing.T, keep int) *config.Config {
	t.Helper()
	return &config.Config{
		VaultDir:   t.TempDir(),
		Extensions: []string{".md"},
		GenKeep:    keep,
	}
}

func writeNote(t *testing.T, cfg *config.Config, name, body string) string {
	t.Helper()
	path := filepath.Join(cfg.NoteDir(), name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func readNote(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

func TestIncrementIsNoOpWhenClean(t *testing.T) {
	cfg := testConfig(t, 10)
	m := New(cfg)
	writeNote(t, cfg, "1.md", "v1\n")

	res, err := m.Increment("")
	if err != nil {
		t.Fatal(err)
	}
	if res.Gen != 1 || !res.Changed {
		t.Fatalf("first increment = %+v, want gen 1 changed", res)
	}

	res, err = m.Increment("")
	if err != nil {
		t.Fatal(err)
	}
	if res.Gen != 1 || res.Changed {
		t.Fatalf("clean increment = %+v, want gen 1 unchanged", res)
	}
}

func TestUndoRedoRoundTrip(t *testing.T) {
	cfg := testConfig(t, 10)
	m := New(cfg)
	path := writeNote(t, cfg, "1.md", "v1\n")
	if _, err := m.Increment(""); err != nil {
		t.Fatal(err)
	}
	writeNote(t, cfg, "1.md", "v2\n")
	if _, err := m.Increment(""); err != nil {
		t.Fatal(err)
	}

	res, err := m.Undo()
	if err != nil {
		t.Fatal(err)
	}
	if res.Gen != 1 || res.Saved != 0 {
		t.Fatalf("undo = %+v, want gen 1 no auto-save", res)
	}
	if got := readNote(t, path); got != "v1\n" {
		t.Fatalf("after undo body = %q, want v1", got)
	}

	res, err = m.Redo()
	if err != nil {
		t.Fatal(err)
	}
	if res.Gen != 2 {
		t.Fatalf("redo = %+v, want gen 2", res)
	}
	if got := readNote(t, path); got != "v2\n" {
		t.Fatalf("after redo body = %q, want v2", got)
	}

	if _, err := m.Redo(); err == nil {
		t.Fatal("redo at head should fail")
	}
}

func TestUndoAtDirtyHeadAutoSaves(t *testing.T) {
	cfg := testConfig(t, 10)
	m := New(cfg)
	path := writeNote(t, cfg, "1.md", "v1\n")
	if _, err := m.Increment(""); err != nil {
		t.Fatal(err)
	}
	writeNote(t, cfg, "1.md", "dirty\n")

	res, err := m.Undo()
	if err != nil {
		t.Fatal(err)
	}
	if res.Gen != 1 || res.Saved != 2 {
		t.Fatalf("undo = %+v, want back on gen 1 with auto-save 2", res)
	}
	if got := readNote(t, path); got != "v1\n" {
		t.Fatalf("after undo body = %q, want v1", got)
	}

	// The discarded working state survived as generation 2; redo revisits it.
	if _, err := m.Redo(); err != nil {
		t.Fatal(err)
	}
	if got := readNote(t, path); got != "dirty\n" {
		t.Fatalf("after redo body = %q, want the auto-saved dirty state", got)
	}
}

func TestUndoOffHeadDiscardsDirty(t *testing.T) {
	cfg := testConfig(t, 10)
	m := New(cfg)
	path := writeNote(t, cfg, "1.md", "v1\n")
	if _, err := m.Increment(""); err != nil {
		t.Fatal(err)
	}
	writeNote(t, cfg, "1.md", "v2\n")
	if _, err := m.Increment(""); err != nil {
		t.Fatal(err)
	}
	writeNote(t, cfg, "1.md", "v3\n")
	if _, err := m.Increment(""); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Undo(); err != nil { // cursor 2
		t.Fatal(err)
	}
	writeNote(t, cfg, "1.md", "dirty off head\n")

	res, err := m.Undo()
	if err != nil {
		t.Fatal(err)
	}
	if res.Gen != 1 || res.Saved != 0 {
		t.Fatalf("off-head undo = %+v, want gen 1 without auto-save", res)
	}
	if got := readNote(t, path); got != "v1\n" {
		t.Fatalf("after undo body = %q, want v1", got)
	}
}

func TestIncrementDropsFutureGenerations(t *testing.T) {
	cfg := testConfig(t, 10)
	m := New(cfg)
	writeNote(t, cfg, "1.md", "v1\n")
	if _, err := m.Increment(""); err != nil {
		t.Fatal(err)
	}
	writeNote(t, cfg, "1.md", "v2\n")
	if _, err := m.Increment(""); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Undo(); err != nil { // cursor 1
		t.Fatal(err)
	}
	writeNote(t, cfg, "1.md", "branch\n")

	res, err := m.Increment("")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Gen != 2 || len(res.Dropped) != 1 || res.Dropped[0] != 2 {
		t.Fatalf("increment = %+v, want new gen 2 dropping old gen 2", res)
	}
	if _, err := m.Redo(); err == nil {
		t.Fatal("redo should fail: history is linear after increment")
	}
}

func TestIncrementPrunesOldGenerations(t *testing.T) {
	cfg := testConfig(t, 2)
	m := New(cfg)
	for _, body := range []string{"v1\n", "v2\n", "v3\n"} {
		writeNote(t, cfg, "1.md", body)
		if _, err := m.Increment(""); err != nil {
			t.Fatal(err)
		}
	}
	list, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Generations) != 2 || list.Generations[0].Gen != 2 || list.Cursor != 3 {
		t.Fatalf("list = %+v, want gens [2 3] cursor 3", list)
	}
}

func TestRestoreRemovesFilesCreatedAfterSnapshot(t *testing.T) {
	cfg := testConfig(t, 10)
	m := New(cfg)
	writeNote(t, cfg, "1.md", "v1\n")
	if _, err := m.Increment(""); err != nil {
		t.Fatal(err)
	}
	writeNote(t, cfg, "2.md", "new note\n")
	if _, err := m.Increment(""); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Undo(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cfg.NoteDir(), "2.md")); !os.IsNotExist(err) {
		t.Fatal("note created after the snapshot should disappear on undo")
	}
}

func TestPeekReadsSnapshotWithoutMovingCursor(t *testing.T) {
	cfg := testConfig(t, 10)
	m := New(cfg)
	writeNote(t, cfg, "1.md", "old\n")
	if _, err := m.Increment(""); err != nil {
		t.Fatal(err)
	}
	writeNote(t, cfg, "1.md", "new\n")
	if _, err := m.Increment(""); err != nil {
		t.Fatal(err)
	}

	got, err := m.Peek(1, "note/1.md")
	if err != nil {
		t.Fatal(err)
	}
	if got != "old\n" {
		t.Fatalf("peek gen 1 = %q, want old", got)
	}
	// Default generation is the cursor.
	got, err = m.Peek(0, "note/1.md")
	if err != nil {
		t.Fatal(err)
	}
	if got != "new\n" {
		t.Fatalf("peek cursor = %q, want new", got)
	}
	list, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	if list.Cursor != 2 {
		t.Fatalf("cursor moved to %d after peek", list.Cursor)
	}
	if _, err := m.Peek(1, "note/9.md"); err == nil {
		t.Fatal("peek of a note missing from the generation should fail")
	}
}

func TestListReportsDirty(t *testing.T) {
	cfg := testConfig(t, 10)
	m := New(cfg)
	writeNote(t, cfg, "1.md", "v1\n")

	list, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	if !list.Dirty || list.Cursor != 0 {
		t.Fatalf("pre-increment list = %+v, want dirty with no cursor", list)
	}

	if _, err := m.Increment(""); err != nil {
		t.Fatal(err)
	}
	list, err = m.List()
	if err != nil {
		t.Fatal(err)
	}
	if list.Dirty {
		t.Fatalf("clean list = %+v, want not dirty", list)
	}

	writeNote(t, cfg, "1.md", "v2\n")
	list, err = m.List()
	if err != nil {
		t.Fatal(err)
	}
	if !list.Dirty {
		t.Fatalf("edited list = %+v, want dirty", list)
	}
}

func TestSidecarMetadataTravelsWithGenerations(t *testing.T) {
	cfg := testConfig(t, 10)
	m := New(cfg)
	writeNote(t, cfg, "1.md", "v1\n")
	metaPath := cfg.MetadataPath(1)
	if err := os.MkdirAll(filepath.Dir(metaPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metaPath, []byte("version: 1\ntitle: Old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Increment(""); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metaPath, []byte("version: 1\ntitle: New\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Increment(""); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Undo(); err != nil {
		t.Fatal(err)
	}
	if got := readNote(t, metaPath); got != "version: 1\ntitle: Old\n" {
		t.Fatalf("sidecar after undo = %q, want the old title", got)
	}
}

func TestIncrementLabelShownInList(t *testing.T) {
	cfg := testConfig(t, 10)
	m := New(cfg)
	writeNote(t, cfg, "1.md", "v1\n")
	if _, err := m.Increment("dream-savepoint"); err != nil {
		t.Fatal(err)
	}
	res, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Generations) != 1 || res.Generations[0].Label != "dream-savepoint" {
		t.Fatalf("list = %+v, want one gen labeled dream-savepoint", res.Generations)
	}
}

func TestStatusReportsAddedChangedDeleted(t *testing.T) {
	cfg := testConfig(t, 10)
	m := New(cfg)
	writeNote(t, cfg, "1.md", "v1\n")
	writeNote(t, cfg, "2.md", "keep\n")
	if _, err := m.Increment(""); err != nil {
		t.Fatal(err)
	}

	writeNote(t, cfg, "1.md", "v2\n")  // changed
	writeNote(t, cfg, "3.md", "new\n") // added
	if err := os.Remove(filepath.Join(cfg.NoteDir(), "2.md")); err != nil {
		t.Fatal(err) // deleted
	}

	st, err := m.Status()
	if err != nil {
		t.Fatal(err)
	}
	if !st.Dirty {
		t.Fatalf("status = %+v, want dirty", st)
	}
	if want := []string{"note/3.md"}; !equalStrs(st.Added, want) {
		t.Fatalf("added = %v, want %v", st.Added, want)
	}
	if want := []string{"note/1.md"}; !equalStrs(st.Changed, want) {
		t.Fatalf("changed = %v, want %v", st.Changed, want)
	}
	if want := []string{"note/2.md"}; !equalStrs(st.Deleted, want) {
		t.Fatalf("deleted = %v, want %v", st.Deleted, want)
	}
}

func equalStrs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

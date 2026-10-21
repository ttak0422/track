package index

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/note"
	tmpl "github.com/ttak0422/track/internal/track/template"
)

func fixtureFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func captureWarnings(t *testing.T, fn func()) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = f
	defer func() { os.Stderr = old; f.Close() }()
	fn()
	raw, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestFullSkipsInvalidFiles(t *testing.T) {
	cfg, s := setup(t)
	writeNote(t, cfg, 100, "original", note.Metadata{Title: "Original"})
	writeNote(t, cfg, 300, "[[Original]]", note.Metadata{Title: "Project-DESKTOP-ABC (conflicted copy)"})
	// The configured extra extension and suffixes in titles/templates remain legitimate.
	cfg.Extensions = append(cfg.Extensions, ".markdown")
	fixtureFile(t, filepath.Join(cfg.NoteDir(), "600.markdown"), "extra extension")
	fixtureFile(t, cfg.PathForKind(config.KindJournal, 20261021), "journal")
	fixtureFile(t, cfg.TemplatePath(700), "<!-- track-template\nname: weekly-review\n-->\n")

	strays := []string{
		filepath.Join(cfg.NoteDir(), "100 (conflicted copy).md"), // ADR 0014's sync example
		filepath.Join(cfg.NoteDir(), "100-DESKTOP-ABC.md"),
		filepath.Join(cfg.NoteDir(), "900 (conflicted copy).md"), // no original; never import it
		filepath.Join(cfg.NoteDir(), "meeting-notes.md"),
		filepath.Join(cfg.NoteDir(), "999999999999999999999999.md"),
		filepath.Join(cfg.JournalDir(), "20261021-PC.md"),
	}
	for _, path := range strays {
		fixtureFile(t, path, "conflict body must never replace the original\n")
	}
	writeNote(t, cfg, 200, "bad sidecar", note.Metadata{Title: "Bad"})
	fixtureFile(t, cfg.MetadataPath(200), "version: 1\ntitle: [\n")
	writeNote(t, cfg, 400, "unsupported sidecar", note.Metadata{Title: "Future"})
	fixtureFile(t, cfg.MetadataPath(400), "version: 999\ntitle: Future\n")
	fixtureFile(t, cfg.NotePath(500), "body\n<!--track\ntitle: [\n-->\n")
	fixtureFile(t, cfg.NotePath(501), "body\n<!--track\nversion: 999\n-->\n")
	// Sidecar conflict copies are never selected in place of the authoritative <id>.yaml.
	conflictMeta := filepath.Join(cfg.MetadataDir(), "100 (conflicted copy).yaml")
	fixtureFile(t, conflictMeta, "version: [\n")

	unchanged := append(slices.Clone(strays), cfg.MetadataPath(200), cfg.MetadataPath(400), cfg.NotePath(500), cfg.NotePath(501), conflictMeta, cfg.TemplatePath(700))
	before := map[string]string{}
	for _, path := range unchanged {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		before[path] = string(raw)
	}
	ix := New(cfg, s)
	var previous string
	for run := 0; run < 2; run++ {
		warnings := captureWarnings(t, func() {
			rep, err := ix.Full()
			if err != nil || rep.Indexed != 4 || rep.Skipped != len(strays)+4 || rep.Deleted != 0 || rep.Links != 1 {
				t.Fatalf("Full = %+v, %v", rep, err)
			}
		})
		if run > 0 && warnings != previous {
			t.Fatalf("diagnostics changed between identical runs:\n%s\n%s", previous, warnings)
		}
		previous = warnings
		for _, path := range append(slices.Clone(strays), cfg.MetadataPath(200), cfg.MetadataPath(400), cfg.NotePath(500), cfg.NotePath(501)) {
			if !strings.Contains(warnings, path) {
				t.Errorf("missing path %q in diagnostics: %s", path, warnings)
			}
		}
		for _, reason := range []string{"invalid note filename", "yaml:", "unsupported metadata version 999", "invalid legacy metadata", "track doctor"} {
			if !strings.Contains(warnings, reason) {
				t.Errorf("missing reason %q: %s", reason, warnings)
			}
		}
	}
	rows, err := s.AllNotes()
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, row := range rows {
		ids = append(ids, row.NoteID)
	}
	slices.Sort(ids)
	if !reflect.DeepEqual(ids, []int64{100, 300, 600, 20261021}) {
		t.Fatalf("unexpected imported ids: %v", ids)
	}
	if hits, err := s.SearchBodyFTS("conflict body", 10); err != nil || len(hits) != 0 {
		t.Fatalf("conflict content leaked into the index: %+v, %v", hits, err)
	}
	if n, err := note.ParseFile(strays[0], cfg); err == nil || n != nil {
		t.Fatalf("single-file parse must still return an error, got %+v, %v", n, err)
	}
	for path, want := range before {
		raw, err := os.ReadFile(path)
		if err != nil || string(raw) != want {
			t.Errorf("skipped/unmanaged file changed: %s: %v", path, err)
		}
	}
	if _, err := os.Stat(cfg.MetadataPath(500)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed legacy parse must not create a sidecar: %v", err)
	}
	templates, err := tmpl.List(cfg)
	if err != nil || len(templates) != 1 || templates[0].Name != "weekly-review" {
		t.Fatalf("legitimate template suffix affected: %+v, %v", templates, err)
	}
}

func TestRefreshSkipsBadMetadataWithoutDeletingCachedNote(t *testing.T) {
	cfg, s := setup(t)
	writeNote(t, cfg, 100, "cached body", note.Metadata{Title: "Cached"})
	writeNote(t, cfg, 200, "to delete", note.Metadata{Title: "Delete"})
	ix := New(cfg, s)
	if _, err := ix.Full(); err != nil {
		t.Fatal(err)
	}
	bad := "version: 1\ntitle: [\n"
	fixtureFile(t, cfg.MetadataPath(100), bad)
	fixtureFile(t, cfg.NotePath(100), "changed but invalid metadata")
	if err := os.Remove(cfg.NotePath(200)); err != nil {
		t.Fatal(err)
	}
	writeNote(t, cfg, 300, "new valid body", note.Metadata{Title: "New"})
	warnings := captureWarnings(t, func() {
		if changed, err := ix.RefreshIfStale(); err != nil || !changed {
			t.Fatalf("RefreshIfStale = %v, %v", changed, err)
		}
	})
	if strings.Count(warnings, "track: warning:") != 1 || !strings.Contains(warnings, cfg.MetadataPath(100)) {
		t.Fatalf("expected one contextual warning: %s", warnings)
	}
	rows, err := s.AllNotes()
	if err != nil || len(rows) != 2 {
		t.Fatalf("cached and new note must survive, deleted note must leave: %+v, %v", rows, err)
	}
	if rows[0].NoteID != 100 || rows[0].Title != "Cached" || rows[1].NoteID != 300 {
		t.Fatalf("unexpected rows: %+v", rows)
	}
	raw, _ := os.ReadFile(cfg.MetadataPath(100))
	if string(raw) != bad {
		t.Fatal("refresh overwrote invalid metadata")
	}
	// The normal rebuild picks the note up again once its metadata is repaired.
	writeNote(t, cfg, 100, "repaired body", note.Metadata{Title: "Repaired"})
	repairedTime := time.Now().Add(time.Hour)
	if err := os.Chtimes(cfg.NotePath(100), repairedTime, repairedTime); err != nil {
		t.Fatal(err)
	}
	if _, err := ix.RefreshIfStale(); err != nil {
		t.Fatal(err)
	}
	rows, _ = s.AllNotes()
	if rows[0].Title != "Repaired" {
		t.Fatalf("repaired note not refreshed: %+v", rows)
	}
}

func TestRefreshWarnsAboutConflictWithUnchangedIndex(t *testing.T) {
	cfg, s := setup(t)
	writeNote(t, cfg, 100, "body", note.Metadata{Title: "Original"})
	ix := New(cfg, s)
	if _, err := ix.Full(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cfg.NoteDir(), "100 (conflicted copy).md")
	fixtureFile(t, path, "conflict")
	warnings := captureWarnings(t, func() {
		if changed, err := ix.RefreshIfStale(); err != nil || changed {
			t.Fatalf("conflict must not cause a rebuild: %v, %v", changed, err)
		}
	})
	if !strings.Contains(warnings, path) || !strings.Contains(warnings, "invalid note filename") {
		t.Fatalf("conflict silently skipped: %s", warnings)
	}
}

func TestDirectoryFailureIsFatal(t *testing.T) {
	cfg, s := setup(t)
	// A root that is a file used to be swallowed as if the directory did not exist.
	fixtureFile(t, cfg.NoteDir(), "not a directory")
	ix := New(cfg, s)
	if _, err := ix.Full(); err == nil {
		t.Fatal("Full swallowed directory failure")
	}
	if _, err := ix.RefreshIfStale(); err == nil {
		t.Fatal("RefreshIfStale swallowed directory failure")
	}
}

func TestFullReportsOnlyInvalidFiles(t *testing.T) {
	cfg, s := setup(t)
	fixtureFile(t, filepath.Join(cfg.NoteDir(), "100 (conflicted copy).md"), "stray")
	rep, err := New(cfg, s).Full()
	if err != nil || rep.Indexed != 0 || rep.Skipped != 1 {
		t.Fatalf("invalid files must not look like an empty clean scan: %+v, %v", rep, err)
	}
}

func TestConflictCopiesCannotBypassEmptyScanGuard(t *testing.T) {
	cfg, s := setup(t)
	ix := New(cfg, s)
	for id := int64(1); id <= emptyScanGuardMin; id++ {
		writeNote(t, cfg, id, "body", note.Metadata{Title: "Cached"})
	}
	if _, err := ix.Full(); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(cfg.NoteDir()); err != nil {
		t.Fatal(err)
	}
	fixtureFile(t, filepath.Join(cfg.NoteDir(), "1 (conflicted copy).md"), "stray")
	if _, err := ix.Full(); err == nil {
		t.Fatal("stray copies bypassed empty-scan guard")
	}
	if rows, err := s.AllNotes(); err != nil || len(rows) != emptyScanGuardMin {
		t.Fatalf("guard lost existing rows: %+v, %v", rows, err)
	}
}

func TestUnreadableVaultFilesAreFatal(t *testing.T) {
	for _, target := range []string{"body", "sidecar", "directory"} {
		t.Run(target, func(t *testing.T) {
			cfg, s := setup(t)
			writeNote(t, cfg, 100, "body", note.Metadata{Title: "Original"})
			path := cfg.NotePath(100)
			if target == "sidecar" {
				path = cfg.MetadataPath(100)
			} else if target == "directory" {
				path = cfg.NoteDir()
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, 0); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.Chmod(path, info.Mode()) })
			_, probeErr := os.ReadFile(path)
			if target == "directory" {
				_, probeErr = os.ReadDir(path)
			}
			if !errors.Is(probeErr, os.ErrPermission) {
				t.Skip("host does not enforce permission fixture")
			}
			if _, err := New(cfg, s).Full(); !errors.Is(err, os.ErrPermission) {
				t.Fatalf("access failure must be fatal, got %v", err)
			}
		})
	}
}

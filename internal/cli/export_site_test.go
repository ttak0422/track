package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeFrontend creates a minimal static-mode frontend build for export-site to copy.
func fakeFrontend(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html><div id=root></div>"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestExportSiteBuildsStaticSite(t *testing.T) {
	vault := t.TempDir()
	if _, code := runIn(t, vault, "new", "--title", "Home", "--id", "100", "--body", "# Home\n\ngo to [[Child]]\n"); code != 0 {
		t.Fatalf("new Home failed")
	}
	if _, code := runIn(t, vault, "new", "--title", "Child", "--id", "200", "--body", "# Child\n\nback [[Home]]\n"); code != 0 {
		t.Fatalf("new Child failed")
	}

	out := filepath.Join(vault, "site")
	res, code := runIn(t, vault, "export-site", "--root", "100", "--id", "200", "--frontend", fakeFrontend(t), "--out", out)
	if code != 0 {
		t.Fatalf("export-site failed: %v", res)
	}
	if res["out"] != out {
		t.Fatalf("unexpected out: %v", res)
	}

	// Frontend copied and data bundle generated.
	if _, err := os.Stat(filepath.Join(out, "index.html")); err != nil {
		t.Fatalf("frontend not copied: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(out, "data", "note", "200.json"))
	if err != nil {
		t.Fatalf("child note bundle missing: %v", err)
	}
	var note struct {
		Note struct {
			Body string `json:"body"`
		} `json:"note"`
		Backlinks []struct {
			NoteID int64 `json:"note_id"`
		} `json:"backlinks"`
	}
	if err := json.Unmarshal(raw, &note); err != nil {
		t.Fatal(err)
	}
	if len(note.Backlinks) != 1 || note.Backlinks[0].NoteID != 100 {
		t.Fatalf("child should have backlink from 100, got %+v", note.Backlinks)
	}
	if !strings.Contains(note.Note.Body, "[[Home]]") {
		t.Fatalf("child body should keep wiki link: %q", note.Note.Body)
	}
}

func TestExportSiteRequiresRoot(t *testing.T) {
	vault := t.TempDir()
	out, code := runIn(t, vault, "export-site", "--frontend", fakeFrontend(t), "--out", filepath.Join(vault, "site"))
	if code != 1 || !strings.Contains(out["error"].(string), "root") {
		t.Fatalf("expected --root required error, got code=%d out=%v", code, out)
	}
}

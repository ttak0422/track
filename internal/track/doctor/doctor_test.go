package doctor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/note"
)

// newVault builds a Config rooted at a temp dir with the note/journal/.track layout in place.
func newVault(t *testing.T) *config.Config {
	t.Helper()
	vault := t.TempDir()
	for _, dir := range []string{"note", "journal", filepath.Join(".track", "notes")} {
		if err := os.MkdirAll(filepath.Join(vault, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return &config.Config{VaultDir: vault, Extensions: []string{".md"}}
}

// writeNote creates a markdown file and, when title != "", its sidecar.
func writeNote(t *testing.T, cfg *config.Config, id int64, title string) {
	t.Helper()
	if err := os.WriteFile(cfg.NotePath(id), []byte("body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if title != "" {
		if err := note.WriteMetadata(cfg.MetadataPath(id), note.Metadata{Title: title}); err != nil {
			t.Fatal(err)
		}
	}
}

func issuesByKind(rep Report) map[IssueKind][]Issue {
	out := map[IssueKind][]Issue{}
	for _, is := range rep.Issues {
		out[is.Kind] = append(out[is.Kind], is)
	}
	return out
}

func TestDiagnoseCleanVault(t *testing.T) {
	cfg := newVault(t)
	writeNote(t, cfg, 100, "Alpha")
	writeNote(t, cfg, 101, "Beta")

	rep, err := Diagnose(cfg)
	if err != nil {
		t.Fatalf("diagnose: %v", err)
	}
	if rep.Scanned != 2 {
		t.Fatalf("scanned = %d, want 2", rep.Scanned)
	}
	if len(rep.Issues) != 0 {
		t.Fatalf("expected no issues, got %v", rep.Issues)
	}
}

func TestDiagnoseMissingSidecar(t *testing.T) {
	cfg := newVault(t)
	writeNote(t, cfg, 100, "") // markdown without sidecar

	rep, err := Diagnose(cfg)
	if err != nil {
		t.Fatalf("diagnose: %v", err)
	}
	got := issuesByKind(rep)[IssueMissingSidecar]
	if len(got) != 1 || got[0].ID != 100 {
		t.Fatalf("missing_sidecar issues = %v", rep.Issues)
	}
}

func TestDiagnoseOrphanSidecar(t *testing.T) {
	cfg := newVault(t)
	if err := note.WriteMetadata(cfg.MetadataPath(200), note.Metadata{Title: "Gone"}); err != nil {
		t.Fatal(err)
	}

	rep, err := Diagnose(cfg)
	if err != nil {
		t.Fatalf("diagnose: %v", err)
	}
	got := issuesByKind(rep)[IssueOrphanSidecar]
	if len(got) != 1 || got[0].ID != 200 {
		t.Fatalf("orphan_sidecar issues = %v", rep.Issues)
	}
}

func TestDiagnoseStrayConflictCopy(t *testing.T) {
	cfg := newVault(t)
	stray := filepath.Join(cfg.NoteDir(), "100 (conflicted copy).md")
	if err := os.WriteFile(stray, []byte("body\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rep, err := Diagnose(cfg)
	if err != nil {
		t.Fatalf("diagnose: %v", err)
	}
	got := issuesByKind(rep)[IssueStrayFile]
	if len(got) != 1 || got[0].Path != stray {
		t.Fatalf("stray_file issues = %v", rep.Issues)
	}
	if rep.Scanned != 0 {
		t.Fatalf("stray file should not count as scanned, got %d", rep.Scanned)
	}
}

func TestDiagnoseDuplicateTitle(t *testing.T) {
	cfg := newVault(t)
	writeNote(t, cfg, 100, "Same")
	writeNote(t, cfg, 101, "Same")

	rep, err := Diagnose(cfg)
	if err != nil {
		t.Fatalf("diagnose: %v", err)
	}
	got := issuesByKind(rep)[IssueDuplicateTitle]
	if len(got) != 1 {
		t.Fatalf("duplicate_title issues = %v", rep.Issues)
	}
}

func TestDiagnoseUnreadableSidecar(t *testing.T) {
	cfg := newVault(t)
	if err := os.WriteFile(cfg.NotePath(100), []byte("body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.MetadataPath(100), []byte("::not yaml::\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rep, err := Diagnose(cfg)
	if err != nil {
		t.Fatalf("diagnose: %v", err)
	}
	got := issuesByKind(rep)[IssueUnreadableSidecar]
	if len(got) != 1 || got[0].ID != 100 {
		t.Fatalf("unreadable_sidecar issues = %v", rep.Issues)
	}
}

func TestDiagnoseMissingVaultDirsIsClean(t *testing.T) {
	// A vault whose note/journal/.track dirs do not exist yet should diagnose cleanly, not error.
	cfg := &config.Config{VaultDir: t.TempDir(), Extensions: []string{".md"}}
	rep, err := Diagnose(cfg)
	if err != nil {
		t.Fatalf("diagnose: %v", err)
	}
	if len(rep.Issues) != 0 || rep.Scanned != 0 {
		t.Fatalf("expected clean empty report, got %+v", rep)
	}
}

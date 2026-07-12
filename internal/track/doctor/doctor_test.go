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

func TestFixRestoresMissingSidecar(t *testing.T) {
	cfg := newVault(t)
	writeNote(t, cfg, 100, "") // md without sidecar

	rep, err := Fix(cfg, 1700000000000)
	if err != nil {
		t.Fatalf("fix: %v", err)
	}
	if !rep.Changed || len(rep.Fixed) != 1 || rep.Fixed[0].Kind != IssueMissingSidecar {
		t.Fatalf("unexpected fix report: %+v", rep)
	}
	meta, found, err := note.ReadMetadata(cfg.MetadataPath(100))
	if err != nil || !found || meta.Title == "" {
		t.Fatalf("sidecar should exist with a title: meta=%+v found=%v err=%v", meta, found, err)
	}
	// Vault is clean afterward.
	diag, _ := Diagnose(cfg)
	if len(diag.Issues) != 0 {
		t.Fatalf("expected clean vault after fix, got %v", diag.Issues)
	}
}

func TestFixRestoresOrphanSidecar(t *testing.T) {
	cfg := newVault(t)
	if err := note.WriteMetadata(cfg.MetadataPath(200), note.Metadata{Title: "Recovered"}); err != nil {
		t.Fatal(err)
	}

	if _, err := Fix(cfg, 1700000000000); err != nil {
		t.Fatalf("fix: %v", err)
	}
	if _, err := os.Stat(cfg.NotePath(200)); err != nil {
		t.Fatalf("orphan sidecar should have its markdown restored: %v", err)
	}
	diag, _ := Diagnose(cfg)
	if len(diag.Issues) != 0 {
		t.Fatalf("expected clean vault after fix, got %v", diag.Issues)
	}
}

func TestFixRenumbersDuplicateTitles(t *testing.T) {
	cfg := newVault(t)
	writeNote(t, cfg, 100, "Same")
	writeNote(t, cfg, 101, "Same")

	rep, err := Fix(cfg, 1700000000000)
	if err != nil {
		t.Fatalf("fix: %v", err)
	}
	if len(rep.Fixed) != 1 || rep.Fixed[0].Kind != IssueDuplicateTitle || rep.Fixed[0].ID != 101 {
		t.Fatalf("should renumber the higher id, keeping the lower: %+v", rep.Fixed)
	}
	low, _, _ := note.ReadMetadata(cfg.MetadataPath(100))
	high, _, _ := note.ReadMetadata(cfg.MetadataPath(101))
	if low.Title != "Same" {
		t.Fatalf("lowest id should keep its title, got %q", low.Title)
	}
	if high.Title == "Same" || high.Title == "" {
		t.Fatalf("higher id should be renumbered, got %q", high.Title)
	}
}

func TestFixImportsStrayFile(t *testing.T) {
	cfg := newVault(t)
	stray := filepath.Join(cfg.NoteDir(), "100 (conflicted copy).md")
	if err := os.WriteFile(stray, []byte("rescued body\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rep, err := Fix(cfg, 1700000000000)
	if err != nil {
		t.Fatalf("fix: %v", err)
	}
	if len(rep.Fixed) != 1 || rep.Fixed[0].Kind != IssueStrayFile {
		t.Fatalf("stray file should be imported: %+v", rep.Fixed)
	}
	if _, err := os.Stat(stray); !os.IsNotExist(err) {
		t.Fatalf("stray file should have been moved, stat err = %v", err)
	}
	imported := rep.Fixed[0].ID
	raw, err := os.ReadFile(cfg.NotePath(imported))
	if err != nil || string(raw) != "rescued body\n" {
		t.Fatalf("imported note should keep its body: %q err=%v", string(raw), err)
	}
	diag, _ := Diagnose(cfg)
	if len(diag.Issues) != 0 {
		t.Fatalf("expected clean vault after import, got %v", diag.Issues)
	}
}

func TestFixSkipsUnreadableSidecar(t *testing.T) {
	cfg := newVault(t)
	if err := os.WriteFile(cfg.NotePath(100), []byte("body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.MetadataPath(100), []byte("::not yaml::\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rep, err := Fix(cfg, 1700000000000)
	if err != nil {
		t.Fatalf("fix: %v", err)
	}
	if len(rep.Skipped) != 1 || rep.Skipped[0].Kind != IssueUnreadableSidecar {
		t.Fatalf("unreadable sidecar should be skipped, not fixed: %+v", rep)
	}
}

func TestDiagnosePropertyViolations(t *testing.T) {
	cfg := newVault(t)
	cfg.Properties = map[string]config.PropSpec{
		"status": {Values: []string{"draft", "done"}},
		"rating": {Type: "number"},
	}
	writeNote(t, cfg, 100, "Alpha")
	body := "status:: waiting\n- rating:: high\nstatus:: draft\n"
	if err := os.WriteFile(cfg.NotePath(100), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// Sidecar props are checked too.
	if err := note.WriteMetadata(cfg.MetadataPath(100), note.Metadata{
		Title: "Alpha",
		Props: map[string]any{"status": "maybe"},
	}); err != nil {
		t.Fatal(err)
	}

	rep, err := Diagnose(cfg)
	if err != nil {
		t.Fatalf("diagnose: %v", err)
	}
	got := issuesByKind(rep)[IssuePropertyViolation]
	if len(got) != 3 {
		t.Fatalf("property violations = %+v, want 3", got)
	}
	for _, issue := range got {
		if issue.ID != 100 || issue.Detail == "" {
			t.Fatalf("unexpected issue %+v", issue)
		}
	}

	// Without a schema the same vault diagnoses clean.
	cfg.Properties = nil
	rep, err = Diagnose(cfg)
	if err != nil {
		t.Fatalf("diagnose without schema: %v", err)
	}
	if len(rep.Issues) != 0 {
		t.Fatalf("expected no issues without schema, got %+v", rep.Issues)
	}
}

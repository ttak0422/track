package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/note"
)

// A cross-vault sweep selects each target through config.LoadAt, so the process's own vault
// selection must survive it. The setenv form this replaced left TRACK_VAULT pointing at whichever
// target sorted last, silently retargeting every later command in the same process.
func TestSweepVaultsLeavesVaultSelectionUnchanged(t *testing.T) {
	defaultVault := t.TempDir()
	rows, code := runWithRegistry(t, defaultVault,
		map[string]string{"work": t.TempDir(), "zzz": t.TempDir()}, "reindex")
	if code != 0 {
		t.Fatalf("reindex sweep failed: %v", rows)
	}
	if got := os.Getenv("TRACK_VAULT"); got != defaultVault {
		t.Fatalf("sweep changed the active vault: want %q, got %q", defaultVault, got)
	}
}

func TestReindexAndSearchContinuePastInvalidFiles(t *testing.T) {
	vault := t.TempDir()
	cfg := &config.Config{VaultDir: vault, Extensions: []string{".md"}}
	if err := os.MkdirAll(cfg.NoteDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"100.md":                   "original body",
		"100 (conflicted copy).md": "conflict body",
		"200.md":                   "bad metadata body",
		"300.md":                   "healthy body after the bad files",
	} {
		if err := os.WriteFile(filepath.Join(cfg.NoteDir(), name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for id, title := range map[int64]string{100: "Original", 200: "Bad", 300: "Healthy"} {
		if err := note.WriteMetadata(cfg.MetadataPath(id), note.Metadata{Title: title}); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(cfg.MetadataPath(200), []byte("version: 1\ntitle: [\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := runIn(t, vault, "reindex")
	if code != 0 || out["indexed"] != float64(2) || out["skipped"] != float64(2) {
		t.Fatalf("reindex failed to report partial progress: code %d, %v", code, out)
	}
	// Search refreshes first; malformed metadata must not abort recordActivity or Full.
	out, code = runIn(t, vault, "search", "--query", "Healthy")
	if code != 0 {
		t.Fatalf("search aborted: %v", out)
	}
	results := out["results"].([]any)
	if len(results) != 1 || results[0].(map[string]any)["note_id"] != float64(300) {
		t.Fatalf("valid note after invalid files is missing: %v", out)
	}
}

package cli

import (
	"os"
	"testing"
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

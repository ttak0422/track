package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRequiresTrackVault(t *testing.T) {
	t.Setenv("TRACK_VAULT", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected TRACK_VAULT error")
	}
	if !strings.Contains(err.Error(), "TRACK_VAULT is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadUsesExplicitTrackVault(t *testing.T) {
	vault := t.TempDir()
	t.Setenv("TRACK_VAULT", vault)
	t.Setenv("TRACK_DB", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.VaultDir != vault {
		t.Fatalf("VaultDir = %q, want %q", cfg.VaultDir, vault)
	}
	wantDB := filepath.Join(vault, ".track", "index.db")
	if cfg.DBPath != wantDB {
		t.Fatalf("DBPath = %q, want %q", cfg.DBPath, wantDB)
	}
}

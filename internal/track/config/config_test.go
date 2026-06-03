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
	cache := t.TempDir()
	t.Setenv("TRACK_VAULT", vault)
	t.Setenv("TRACK_DB", "")
	t.Setenv("TRACK_CACHE_DIR", cache)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.VaultDir != vault {
		t.Fatalf("VaultDir = %q, want %q", cfg.VaultDir, vault)
	}
	wantDB := filepath.Join(cache, vaultCacheKey(vault), "index.db")
	if cfg.DBPath != wantDB {
		t.Fatalf("DBPath = %q, want %q", cfg.DBPath, wantDB)
	}
}

func TestLoadHonorsExplicitTrackDB(t *testing.T) {
	vault := t.TempDir()
	db := filepath.Join(t.TempDir(), "custom.db")
	t.Setenv("TRACK_VAULT", vault)
	t.Setenv("TRACK_DB", db)
	t.Setenv("TRACK_CACHE_DIR", t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.DBPath != db {
		t.Fatalf("DBPath = %q, want %q", cfg.DBPath, db)
	}
}

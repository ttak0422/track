package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVaultLocationNeverReadsVaultConfig(t *testing.T) {
	root := t.TempDir()
	vault := filepath.Join(root, "vault")
	if err := os.MkdirAll(filepath.Join(vault, ".track"), 0o700); err != nil {
		t.Fatal(err)
	}
	// Invalid vault YAML is canonical user data; backing it up must not require
	// parsing it or auditing/hydrating every id-v1 sidecar.
	os.WriteFile(filepath.Join(vault, ".track/config.yml"), []byte("invalid: [\n"), 0o600)
	t.Setenv("TRACK_CONFIG", filepath.Join(root, "missing.yml"))
	t.Setenv("TRACK_VAULT", vault)
	location, registry, err := VaultLocation()
	if err != nil || location != vault || len(registry) != 0 {
		t.Fatalf("location=%s registry=%v err=%v", location, registry, err)
	}
	if _, err := Load(); err == nil {
		t.Fatal("fixture should fail full config loading")
	}
}

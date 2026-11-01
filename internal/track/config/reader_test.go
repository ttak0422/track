package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOwnedConfigurationReaderCannotOpenIndexOrAuthorizeUnknownNamespace(t *testing.T) {
	root := t.TempDir()
	cache := t.TempDir()
	machine := filepath.Join(t.TempDir(), "machine.yml")
	os.MkdirAll(filepath.Join(root, ".track", "notes"), 0700)
	os.WriteFile(filepath.Join(root, ".track", "config.yml"), []byte("link_format: id-v1\n"), 0600)
	bad := filepath.Join(root, ".track", "notes", "1.yaml")
	original := []byte("version: 1\ntitle: 'id:123'\n")
	os.WriteFile(bad, original, 0600)
	os.WriteFile(machine, []byte("vault_dir: "+root+"\ncache_dir: "+cache+"\n"), 0600)
	t.Setenv("TRACK_CONFIG", machine)
	t.Setenv("TRACK_VAULT", root)
	t.Setenv("TRACK_CACHE_DIR", "")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	provisional, err := LoadBounded(ctx, "", false)
	if err != nil || provisional == nil || !provisional.Provisional {
		t.Fatalf("read-only config unavailable: %+v %v", provisional, err)
	}
	if _, err := LoadBounded(ctx, "", true); err == nil {
		t.Fatal("unverified ID namespace authorized writes")
	}
	if _, err := os.Stat(provisional.DBPath); !os.IsNotExist(err) {
		t.Fatal("configuration worker opened index", err)
	}
	raw, _ := os.ReadFile(bad)
	if string(raw) != string(original) {
		t.Fatal("config worker mutated sidecar")
	}
}

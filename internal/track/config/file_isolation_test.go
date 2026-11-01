package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestIdentityAuditContinuesPastMalformedSidecar(t *testing.T) {
	vault := t.TempDir()
	t.Setenv("TRACK_CONFIG", filepath.Join(t.TempDir(), "missing.yml"))
	t.Setenv("TRACK_CACHE_DIR", t.TempDir())
	dir := filepath.Join(vault, ".track", "notes")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(VaultConfigPath(vault), []byte("link_format: id-v1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "100.yaml"), []byte("version: 1\ntitle: [\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "200.yaml"), []byte("version: 1\ntitle: Healthy\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadAt(vault)
	if err != nil {
		t.Fatalf("one malformed sidecar blocked native startup: %v", err)
	}
	if err := cfg.ValidateLinkFormat(); err != nil {
		t.Fatalf("one malformed sidecar blocked normal requests: %v", err)
	}
	// Unknown titles cannot authorize an explicit ID against an unauditable namespace.
	if err := cfg.ValidateExplicitTarget("id:200", "id-v1"); err == nil {
		t.Fatal("explicit ID accepted despite an unreadable title namespace")
	}
}

func TestIdentityAuditIsolatesFilePermissionsButNotDirectoryFailure(t *testing.T) {
	for _, target := range []string{"file", "directory"} {
		t.Run(target, func(t *testing.T) {
			vault := t.TempDir()
			t.Setenv("TRACK_CONFIG", filepath.Join(t.TempDir(), "missing.yml"))
			t.Setenv("TRACK_CACHE_DIR", t.TempDir())
			dir := filepath.Join(vault, ".track", "notes")
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(VaultConfigPath(vault), []byte("link_format: id-v1\n"), 0644); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "100.yaml")
			if err := os.WriteFile(path, []byte("version: 1\ntitle: Healthy\n"), 0644); err != nil {
				t.Fatal(err)
			}
			if target == "directory" {
				path = dir
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, 0); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.Chmod(path, info.Mode()) })
			_, err = os.ReadFile(path)
			if target == "directory" {
				_, err = os.ReadDir(path)
			}
			if !errors.Is(err, os.ErrPermission) {
				t.Skip("host does not enforce permission fixture")
			}
			_, err = LoadAt(vault)
			if target == "file" && err != nil {
				t.Fatalf("one unreadable sidecar blocked startup: %v", err)
			}
			if target == "directory" && !errors.Is(err, os.ErrPermission) {
				t.Fatalf("metadata directory outage hidden: %v", err)
			}
		})
	}
}

func TestIdentityAuditIgnoresUnmanagedSidecarCopies(t *testing.T) {
	vault := t.TempDir()
	dir := filepath.Join(vault, ".track", "notes")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "100 (conflicted copy).yaml"), []byte("version: 1\ntitle: id:100\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := auditLinkFormat(vault, "id-v1", nil); err != nil {
		t.Fatalf("unmanaged conflict copy blocked startup: %v", err)
	}
}

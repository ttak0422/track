package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinkFormatActivationAudit(t *testing.T) {
	vault := t.TempDir()
	t.Setenv("TRACK_CONFIG", filepath.Join(t.TempDir(), "missing.yml"))
	t.Setenv("TRACK_CACHE_DIR", t.TempDir())
	if err := os.MkdirAll(filepath.Join(vault, ".track", "notes"), 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(vault, ".track", "notes", "123.yaml")
	if err := os.WriteFile(path, []byte("title: id:123\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if cfg, err := LoadAt(vault); err != nil || cfg.LinkFormat != "" {
		t.Fatalf("legacy must remain usable: %+v %v", cfg, err)
	}
	conf := VaultConfigPath(vault)
	if err := os.WriteFile(conf, []byte("link_format: id-v1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAt(vault); err == nil || !strings.Contains(err.Error(), "reserves id:") {
		t.Fatalf("collision not rejected: %v", err)
	}
	if err := os.WriteFile(path, []byte("title: Normal\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if cfg, err := LoadAt(vault); err != nil || cfg.LinkFormat != "id-v1" {
		t.Fatalf("activation: %+v %v", cfg, err)
	}
	// A later synced collision must fail the next load, not silently become an ID reference.
	if err := os.WriteFile(path, []byte("title: work:id:123\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAt(vault); err == nil {
		t.Fatal("synced collision accepted")
	}
	if err := auditLinkFormat(vault, "id-v1", map[string]string{"id": "/other"}); err == nil {
		t.Fatal("reserved vault name accepted")
	}
	if err := validateVaultConfig(vaultFileConfig{LinkFormat: "future"}); err == nil {
		t.Fatal("unknown format accepted")
	}
}

func TestIdentityActivationDoesNotRequireUnrelatedVaults(t *testing.T) {
	source := t.TempDir()
	if err := auditLinkFormat(source, "id-v1", map[string]string{"off": filepath.Join(t.TempDir(), "missing")}); err != nil {
		t.Fatalf("unrelated unavailable target blocked local work: %v", err)
	}
}

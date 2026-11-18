package webui

import (
	"path/filepath"
	"testing"

	"github.com/ttak0422/track/internal/track/config"
)

func TestInternalReadPolicyProtectionSnapshotsDoNotNarrowOrAlias(t *testing.T) {
	root := t.TempDir()
	files := []string{filepath.Join(root, "machine.yml")}
	p, err := newReadPolicy(filepath.Join(root, "private"), files)
	if err != nil {
		t.Fatal(err)
	}
	files[0] = "changed"
	if p.configurationFiles[0] == "changed" {
		t.Fatal("caller mutated policy")
	}
	launch := filepath.Join(root, "launch")
	peer := filepath.Join(root, "peer")
	cfg := &config.Config{VaultDir: launch, Vaults: map[string]string{"peer": peer}}
	first := p.withConfig(cfg)
	cfg.Vaults["peer"] = filepath.Join(root, "replacement")
	next := first.withConfig(cfg)
	if len(p.vaults) != 0 || len(first.vaults) != 2 || len(next.vaults) != 3 {
		t.Fatal("protection set narrowed or old policy mutated")
	}
	if first.vaults[1] != peer || next.vaults[1] != peer {
		t.Fatal("old peer lost")
	}
	if _, err := newReadPolicy("", files); err == nil {
		t.Fatal("implicit cache root admitted")
	}
}

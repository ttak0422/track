package vaultref

import (
	"path/filepath"
	"testing"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

// The active vault is in its own registry (ADR 0051), so its own name is not a qualifier: a
// reference written that way is an ordinary local link, and its backlink is a plain one. Inbound
// must therefore skip the active vault — scanning it would only resurface stale self ext_links rows
// left by indexes built before that was true, and reopen our own DB to do it.
func TestInboundSkipsTheActiveVault(t *testing.T) {
	vault := t.TempDir()
	cfg := &config.Config{
		VaultDir:   vault,
		DBPath:     filepath.Join(vault, ".track", "index.db"),
		Extensions: []string{".md"},
		VaultName:  "me",
		Vaults:     map[string]string{"me": vault},
	}
	s, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	if err := s.UpsertNote(&note.Note{ID: 1, Path: cfg.NotePath(1), Meta: note.Metadata{Title: "Source"}}); err != nil {
		t.Fatalf("upsert note: %v", err)
	}
	// A stale self edge, of the shape older indexes wrote for [[me:Target]].
	if err := s.ReplaceExtLinks(1, []store.ExtRef{{Vault: "me", Title: "Target"}}); err != nil {
		t.Fatalf("replace ext links: %v", err)
	}

	r := New(cfg)
	defer r.Close()
	refs, unavailable := r.Inbound("Target")
	if len(refs) != 0 {
		t.Fatalf("the active vault's own edges are the plain backlinks, got %+v", refs)
	}
	if len(unavailable) != 0 {
		t.Fatalf("no other vault is registered, so nothing can be unavailable, got %+v", unavailable)
	}
	if r.IsVault("me") {
		t.Fatal("the active vault's own name must not read as a cross-vault qualifier")
	}
}

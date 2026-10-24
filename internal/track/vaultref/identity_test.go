package vaultref

import (
	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
	"path/filepath"
	"testing"
)

func TestIdentityUsesSourceModeAndVaultLocalID(t *testing.T) {
	source := &config.Config{LinkFormat: "id-v1", VaultName: "source", Vaults: map[string]string{"work": "/target"}}
	target := &config.Config{VaultDir: t.TempDir(), Extensions: []string{".md"}}
	s, err := store.Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertNote(&note.Note{ID: 1, Meta: note.Metadata{Title: "Target in work"}}); err != nil {
		t.Fatal(err)
	}

	r := New(source)
	r.cfgs["work"] = target
	r.stores["work"] = s
	defer r.Close()
	res, found, err := r.Resolve("work", "id:1")
	if err != nil || !found || res.NoteID != 1 || res.Vault != "work" {
		t.Fatalf("source id mode => %+v %v %v", res, found, err)
	}
	if _, found, err := r.Resolve("work", "id:999"); err != nil || found {
		t.Fatalf("missing ID fallback: %v %v", found, err)
	}
	if err := s.UpsertNote(&note.Note{ID: 2, Meta: note.Metadata{Title: "id:1"}}); err != nil {
		t.Fatal(err)
	}
	if _, found, err := r.Resolve("work", "id:1"); err == nil || found {
		t.Fatalf("remote legacy title collision did not fail closed: %v %v", found, err)
	}
	source.LinkFormat = ""
	res, found, err = r.Resolve("work", "id:1")
	if err != nil || !found || res.NoteID != 2 {
		t.Fatalf("legacy source title => %+v %v %v", res, found, err)
	}
}

func TestInboundIdentitySurvivesTargetRenameAndDeduplicates(t *testing.T) {
	cfg := &config.Config{VaultDir: t.TempDir(), DBPath: filepath.Join(t.TempDir(), "index.db"), VaultName: "target", Vaults: map[string]string{"source": "/source", "legacy": "/legacy"}}
	local, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer local.Close()
	if err := local.UpsertNote(&note.Note{ID: 1, Meta: note.Metadata{Title: "Renamed"}}); err != nil {
		t.Fatal(err)
	}
	r := New(cfg)
	defer r.Close()
	for _, name := range []string{"source", "legacy"} {
		s, err := store.Open(filepath.Join(t.TempDir(), "index.db"))
		if err != nil {
			t.Fatal(err)
		}
		if err := s.UpsertNote(&note.Note{ID: 7, Meta: note.Metadata{Title: name}}); err != nil {
			t.Fatal(err)
		}
		refs := []store.ExtRef{{Vault: "target", Title: "id:1"}}
		if name == "source" {
			refs = []store.ExtRef{{Vault: "target", TargetID: 1}, {Vault: "target", Title: "Renamed"}}
		}
		if err := s.ReplaceExtLinks(7, refs); err != nil {
			t.Fatal(err)
		}
		format := ""
		if name == "source" {
			format = "id-v1"
		}
		r.cfgs[name] = &config.Config{LinkFormat: format, VaultDir: t.TempDir(), Extensions: []string{".md"}}
		r.stores[name] = s
	}
	refs, unavailable := r.Inbound("Renamed")
	if len(unavailable) != 0 || len(refs) != 1 || refs[0].Vault != "source" {
		t.Fatalf("inbound = %+v unavailable %+v", refs, unavailable)
	}
	if err := r.stores["source"].ReplaceExtLinks(7, []store.ExtRef{{Vault: "target", TargetID: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := local.UpsertNote(&note.Note{ID: 2, Meta: note.Metadata{Title: "id:1"}}); err != nil {
		t.Fatal(err)
	}
	refs, unavailable = r.Inbound("Renamed")
	if len(refs) != 0 || len(unavailable) != 0 {
		t.Fatalf("ambiguous typed backlink appeared: %+v %+v", refs, unavailable)
	}

}

func TestIdentityResolutionChecksSyncedTargetSidecarsBeforeStaleIndex(t *testing.T) {
	source := &config.Config{LinkFormat: "id-v1", Vaults: map[string]string{"work": "unused"}}
	target := &config.Config{VaultDir: t.TempDir(), Extensions: []string{".md"}}
	s, err := store.Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertNote(&note.Note{ID: 1, Meta: note.Metadata{Title: "ID target"}}); err != nil {
		t.Fatal(err)
	}
	r := New(source)
	r.cfgs["work"] = target
	r.stores["work"] = s
	defer r.Close()
	if _, found, err := r.Resolve("work", "id:1"); err != nil || !found {
		t.Fatalf("initial target: %v %v", found, err)
	}
	// A synced sidecar appears after handles were cached; its vault has not reindexed.
	if err := note.WriteMetadata(target.MetadataPath(2), note.Metadata{Title: "id:1"}); err != nil {
		t.Fatal(err)
	}
	if _, found, err := r.Resolve("work", "id:1"); err == nil || found {
		t.Fatalf("stale remote index hid collision: %v %v", found, err)
	}
}

package store

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/ttak0422/track/internal/track/note"
)

// seedVaultDB creates one vault's index DB with the given notes and returns its path.
func seedVaultDB(t *testing.T, notes []*note.Note) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "index.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	for _, n := range notes {
		if err := s.UpsertNote(n); err != nil {
			t.Fatalf("upsert %d: %v", n.ID, err)
		}
	}
	return dbPath
}

func TestFederatedSearchLabelsAndMerges(t *testing.T) {
	// The same id in two vaults must stay two distinct results — the (vault, id) identity.
	main := seedVaultDB(t, []*note.Note{
		{ID: 1, Kind: "note", Mtime: 300, Body: "alpha body", Meta: note.Metadata{Title: "Alpha main", Tags: []string{"project"}}},
		{ID: 2, Kind: "note", Mtime: 100, Body: "unrelated", Meta: note.Metadata{Title: "Other"}},
	})
	work := seedVaultDB(t, []*note.Note{
		{ID: 1, Kind: "note", Mtime: 200, Body: "alpha in work", Meta: note.Metadata{Title: "Alpha work"}},
	})

	fed, err := OpenFederated([]FederatedVault{{Name: "", DBPath: main}, {Name: "work", DBPath: work}})
	if err != nil {
		t.Fatalf("open federated: %v", err)
	}
	defer fed.Close()

	results, err := fed.Search("Alpha", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("want both vaults' Alphas, got %+v", results)
	}
	// Equal title-rank, so recency orders: main's Alpha (mtime 300) before work's (200).
	if results[0].Vault != "" || results[0].Title != "Alpha main" {
		t.Fatalf("first hit should be the active vault's, got %+v", results[0])
	}
	if results[1].Vault != "work" || results[1].NoteID != 1 {
		t.Fatalf("second hit should be work's id 1, got %+v", results[1])
	}
	if len(results[0].Tags) != 1 || results[0].Tags[0] != "project" {
		t.Fatalf("tags must come from the hit's own vault, got %+v", results[0])
	}

	// Tag queries filter per vault: only main's note carries #project.
	tagged, err := fed.Search("#project", 10)
	if err != nil || len(tagged) != 1 || tagged[0].Vault != "" || tagged[0].NoteID != 1 {
		t.Fatalf("tag search should hit main's note only, got %+v err=%v", tagged, err)
	}
}

func TestFederatedSearchBodyFTS(t *testing.T) {
	main := seedVaultDB(t, []*note.Note{
		{ID: 10, Kind: "note", Mtime: 100, Body: "the needle is here", Meta: note.Metadata{Title: "Main note"}},
	})
	work := seedVaultDB(t, []*note.Note{
		{ID: 20, Kind: "note", Mtime: 200, Body: "another needle text", Meta: note.Metadata{Title: "Work note"}},
	})

	fed, err := OpenFederated([]FederatedVault{{Name: "", DBPath: main}, {Name: "work", DBPath: work}})
	if err != nil {
		t.Fatalf("open federated: %v", err)
	}
	defer fed.Close()

	results, err := fed.SearchBodyFTS("needle", 10)
	if err != nil {
		t.Fatalf("body fts: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("want hits from both vaults, got %+v", results)
	}
	vaults := map[string]int64{}
	for _, r := range results {
		vaults[r.Vault] = r.NoteID
	}
	if vaults[""] != 10 || vaults["work"] != 20 {
		t.Fatalf("unexpected vault labels: %+v", results)
	}
}

func TestOpenFederatedSkipsWhatItCannotAttach(t *testing.T) {
	// SQLite attaches at most 10 databases. Past that — or with an unreadable index — the vault must
	// drop out of the query and be reported, not take the whole cross-vault search down with it.
	dir := t.TempDir()
	var vaults []FederatedVault
	for i := range 14 {
		path := filepath.Join(dir, fmt.Sprintf("v%d.db", i))
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		s.Close()
		vaults = append(vaults, FederatedVault{Name: fmt.Sprintf("v%d", i), DBPath: path})
	}

	fed, err := OpenFederated(vaults)
	if err != nil {
		t.Fatalf("attaching past the limit must degrade, not fail: %v", err)
	}
	defer fed.Close()
	if len(fed.Skipped()) == 0 {
		t.Fatal("the vaults that could not be attached must be reported")
	}
	if _, err := fed.Recent(10); err != nil {
		t.Fatalf("the attached vaults must still answer: %v", err)
	}
}

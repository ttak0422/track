package index

import (
	"github.com/ttak0422/track/internal/track/note"
	"testing"
)

func TestIdentityGraphOptInRefreshRenameAndLegacy(t *testing.T) {
	cfg, s := setup(t)
	cfg.JournalOff = true
	cfg.VaultName = "me"
	cfg.Vaults = map[string]string{"me": cfg.VaultDir, "work": "/not-opened"}
	writeNote(t, cfg, 1, "## Heading\n\nparagraph ^part", note.Metadata{Title: "Target"})
	writeNote(t, cfg, 2, "body", note.Metadata{Title: "1"})
	writeNote(t, cfg, 3, "[[id:1|stale title]] ![[id:1#^part]] [[me:id:1##Heading]] [[1]] [[work:id:1|Target]] [[id:999|Target]]", note.Metadata{Title: "Source"})
	ix := New(cfg, s)
	if _, err := ix.Full(); err != nil {
		t.Fatal(err)
	}
	if backs, _ := s.Backlinks(1); len(backs) != 0 {
		t.Fatalf("legacy interpreted ID: %+v", backs)
	}
	cfg.LinkFormat = "id-v1"
	if changed, err := ix.RefreshIfStale(); err != nil || !changed {
		t.Fatalf("activation did not refresh: %v %v", changed, err)
	}
	if backs, _ := s.Backlinks(1); len(backs) != 1 || backs[0].NoteID != 3 {
		t.Fatalf("ID backlinks: %+v", backs)
	}
	if backs, _ := s.Backlinks(2); len(backs) != 1 || backs[0].NoteID != 3 {
		t.Fatalf("bare number was not title: %+v", backs)
	}
	if backs, _ := s.ExtBacklinksTo([]string{"work"}, "Target", 1); len(backs) != 1 {
		t.Fatalf("external ID edge missing: %+v", backs)
	}
	writeNote(t, cfg, 1, "## Heading\n\nparagraph ^part", note.Metadata{Title: "Renamed"})
	if _, err := ix.Full(); err != nil {
		t.Fatal(err)
	}
	if backs, _ := s.Backlinks(1); len(backs) != 1 {
		t.Fatalf("ID broken by rename: %+v", backs)
	}
	cfg.LinkFormat = ""
	if changed, err := ix.RefreshIfStale(); err != nil || !changed {
		t.Fatalf("deactivation did not refresh: %v %v", changed, err)
	}
	if backs, _ := s.Backlinks(1); len(backs) != 0 {
		t.Fatalf("stale ID edge after deactivation: %+v", backs)
	}
}

package index

import (
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
	"os"
	"testing"
	"time"
)

func TestPartialReadPreservesMetadataAndLinksAndContinues(t *testing.T) {
	for _, failure := range []string{"body", "metadata"} {
		t.Run(failure, func(t *testing.T) {
			cfg, s := setup(t)
			for id, title := range []string{"A", "B", "C"} {
				writeNote(t, cfg, int64(id+1), "[[A]] [[B]] [[C]]", note.Metadata{Title: title, Tags: []string{"kept"}})
			}
			ix := New(cfg, s)
			if _, err := ix.Full(); err != nil {
				t.Fatal(err)
			}
			var restore func()
			if failure == "body" {
				if err := os.Chmod(cfg.NotePath(2), 0); err != nil {
					t.Fatal(err)
				}
				restore = func() { os.Chmod(cfg.NotePath(2), 0644) }
				if _, err := os.ReadFile(cfg.NotePath(2)); err == nil {
					restore()
					t.Skip("permission fixture not enforced")
				}
			} else {
				if err := os.Rename(cfg.MetadataPath(2), cfg.MetadataPath(2)+".held"); err != nil {
					t.Fatal(err)
				}
				restore = func() { os.Rename(cfg.MetadataPath(2)+".held", cfg.MetadataPath(2)) }
			}
			defer restore()
			writeNote(t, cfg, 3, "[[A]] [[B]]", note.Metadata{Title: "Changed", Tags: []string{"new"}})
			rep, err := ix.Full()
			if err != nil || rep.ReadState == nil || !rep.ReadState.CacheUsed {
				t.Fatalf("%+v %v", rep, err)
			}
			for _, title := range []string{"A", "B", "Changed"} {
				if _, found, err := s.ResolveTerm(title); err != nil || !found {
					t.Fatalf("lost %s %v", title, err)
				}
			}
			tags, err := s.SearchScoped("#kept", 10, store.SearchTitle)
			if err != nil || len(tags) != 2 {
				t.Fatalf("lost cached tags: %+v %v", tags, err)
			}
			back, err := s.Backlinks(2)
			if err != nil || len(back) != 2 {
				t.Fatalf("lost target links: %+v %v", back, err)
			}
			back, err = s.Backlinks(1)
			if err != nil || len(back) != 2 {
				t.Fatalf("lost source links: %+v %v", back, err)
			}
			if _, err := ix.ReconcileMissing(); err == nil {
				t.Fatal("pruned partial read")
			}
			restore()
			rep, err = ix.Full()
			if err != nil || rep.ReadState != nil {
				t.Fatalf("recovery did not clear status: %+v %v", rep, err)
			}
		})
	}
}

func TestPartialFirstRunAndPersistentStatus(t *testing.T) {
	cfg, s := setup(t)
	writeNote(t, cfg, 1, "readable", note.Metadata{Title: "Healthy"})
	writeNote(t, cfg, 2, "unreadable", note.Metadata{Title: "Unavailable"})
	if err := os.Chmod(cfg.NotePath(2), 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(cfg.NotePath(2), 0644)
	if _, err := os.ReadFile(cfg.NotePath(2)); err == nil {
		t.Skip("permission fixture not enforced")
	}
	rep, err := New(cfg, s).Full()
	if err != nil || rep.Indexed != 1 || rep.ReadState == nil || rep.ReadState.CacheUsed {
		t.Fatalf("first-run status %+v %v", rep, err)
	}
	other, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	state, err := other.ReadState()
	if err != nil || !state.Partial || state.CheckedAt < time.Now().Add(-time.Minute).UnixMilli() {
		t.Fatalf("status lost across connections: %+v %v", state, err)
	}
}

func TestPruneRejectsRootDisappearanceDuringActualScan(t *testing.T) {
	cfg, s := setup(t)
	writeNote(t, cfg, 1, "body", note.Metadata{Title: "Keep"})
	ix := New(cfg, s)
	if _, err := ix.Full(); err != nil {
		t.Fatal(err)
	}
	ix.readDir = func(path string) ([]os.DirEntry, error) {
		if path == cfg.NoteDir() {
			if err := os.Rename(path, path+".held"); err != nil {
				t.Fatal(err)
			}
		}
		return os.ReadDir(path)
	}
	if _, err := ix.ReconcileMissing(); err == nil {
		t.Fatal("pruned root that disappeared during actual scan")
	}
	if _, found, err := s.ResolveTerm("Keep"); err != nil || !found {
		t.Fatalf("cache lost: %v %v", found, err)
	}
}

func TestPruneRechecksAFileRestoredAfterEnumeration(t *testing.T) {
	cfg, s := setup(t)
	writeNote(t, cfg, 1, "body", note.Metadata{Title: "Keep"})
	ix := New(cfg, s)
	if _, err := ix.Full(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(cfg.NotePath(1), cfg.NotePath(1)+".held"); err != nil {
		t.Fatal(err)
	}
	ix.readDir = func(path string) ([]os.DirEntry, error) {
		entries, err := os.ReadDir(path)
		if path == cfg.NoteDir() {
			if err := os.Rename(cfg.NotePath(1)+".held", cfg.NotePath(1)); err != nil {
				t.Fatal(err)
			}
		}
		return entries, err
	}
	if _, err := ix.ReconcileMissing(); err == nil {
		t.Fatal("pruned a restored file")
	}
	if _, found, err := s.ResolveTerm("Keep"); err != nil || !found {
		t.Fatalf("cache lost: %v %v", found, err)
	}
}

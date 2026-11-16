package index

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

// Exclude the derived SQLite cache, but inventory every canonical file and its
// modification time, plus directory membership (including missing metadata),
// under note, journal, and sidecar roots in these disposable fixtures.
func canonicalInventory(t *testing.T, cfg *config.Config) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, root := range []string{cfg.NoteDir(), cfg.JournalDir(), filepath.Dir(cfg.MetadataPath(1))} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			if err != nil {
				return err
			}
			if d.IsDir() {
				out[path] = "directory"
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			out[path] = info.ModTime().Format(time.RFC3339Nano) + "\x00" + string(data)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func TestFinalizeReadNeverAppliesActivityOrCreatesMissingMetadata(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "activity", true: "missing_metadata"}[missing], func(t *testing.T) {
			cfg, st := setup(t)
			writeNote(t, cfg, 1, "body", note.Metadata{Title: "One"})
			if missing {
				if err := os.Remove(cfg.MetadataPath(1)); err != nil {
					t.Fatal(err)
				}
			}
			ix := New(cfg, st)
			snap, err := ix.Stage(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(snap.Notes) != 1 {
				t.Fatalf("stage: %+v", snap)
			}
			snap.Activity[1] = snap.Notes[0].Mtime // Would add a day through ApplyActivity.
			before := canonicalInventory(t, cfg)
			if err := ix.FinalizeRead(context.Background(), snap); err != nil {
				t.Fatal(err)
			}
			if snap.State.Partial || len(snap.Notes) != 1 {
				t.Fatalf("healthy input rejected: %+v", snap)
			}
			if err := ix.Publish(snap); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, canonicalInventory(t, cfg)) {
				t.Fatal("canonical files changed")
			}
			refs, err := st.AllNotes()
			if err != nil || len(refs) != 1 {
				t.Fatal(refs, err)
			}
		})
	}
}

func TestFinalizeReadRejectsChangedInputsAndPublishesOnlyVerifiedSubset(t *testing.T) {
	for _, change := range []string{"body", "sidecar", "removed_sidecar", "added_sidecar"} {
		t.Run(change, func(t *testing.T) {
			cfg, st := setup(t)
			writeNote(t, cfg, 1, "healthy", note.Metadata{Title: "Healthy"})
			writeNote(t, cfg, 2, "staged", note.Metadata{Title: "Staged"})
			if change == "added_sidecar" {
				if err := os.Remove(cfg.MetadataPath(2)); err != nil {
					t.Fatal(err)
				}
			}
			ix := New(cfg, st)
			snap, err := ix.Stage(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "body":
				err = os.WriteFile(cfg.NotePath(2), []byte("different body length"), 0600)
			case "sidecar", "added_sidecar":
				err = note.WriteMetadata(cfg.MetadataPath(2), note.Metadata{Title: "Changed metadata"})
			case "removed_sidecar":
				err = os.Remove(cfg.MetadataPath(2))
			}
			if err != nil {
				t.Fatal(err)
			}
			before := canonicalInventory(t, cfg)
			if err := ix.FinalizeRead(context.Background(), snap); err != nil {
				t.Fatal(err)
			}
			if !snap.State.Partial || len(snap.Notes) != 1 || snap.Notes[0].ID != 1 {
				t.Fatalf("unverified input retained: %+v", snap)
			}
			if err := ix.Publish(snap); err != nil {
				t.Fatal(err)
			}
			refs, err := st.AllNotes()
			if err != nil || len(refs) != 1 || refs[0].NoteID != 1 {
				t.Fatal(refs, err)
			}
			if !reflect.DeepEqual(before, canonicalInventory(t, cfg)) {
				t.Fatal("canonical files changed")
			}
		})
	}
}

func TestFinalizeReadCancellationPartialAndRevision(t *testing.T) {
	for _, mode := range []string{"cancel", "partial", "stale_before", "stale_after"} {
		t.Run(mode, func(t *testing.T) {
			cfg, st := setup(t)
			writeNote(t, cfg, 1, "body", note.Metadata{Title: "One"})
			ix := New(cfg, st)
			snap, err := ix.Stage(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			snap.Activity[1] = snap.Notes[0].Mtime
			before := canonicalInventory(t, cfg)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancel" {
				cancel()
			}
			if mode == "partial" {
				snap.State.Add(store.ReadIssue{Path: "synthetic", Reason: "unavailable peer"})
			}
			if mode == "stale_before" {
				if err := st.InvalidateSnapshot(); err != nil {
					t.Fatal(err)
				}
			}
			err = ix.FinalizeRead(ctx, snap)
			if mode == "stale_before" {
				if !errors.Is(err, ErrSuperseded) {
					t.Fatalf("stale finalization: %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if mode == "cancel" && (!snap.State.Partial || len(snap.Notes) != 0) {
					t.Fatalf("cancelled input admitted: %+v", snap)
				}
				if mode == "partial" && (!snap.State.Partial || len(snap.Notes) != 1) {
					t.Fatalf("partial healthy read lost: %+v", snap)
				}
				if mode == "stale_after" {
					if err := st.InvalidateSnapshot(); err != nil {
						t.Fatal(err)
					}
				}
				err = ix.Publish(snap)
				if mode == "stale_after" {
					if !errors.Is(err, ErrSuperseded) {
						t.Fatalf("stale publish: %v", err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
			}
			if !reflect.DeepEqual(before, canonicalInventory(t, cfg)) {
				t.Fatal("canonical files changed")
			}
		})
	}
}

func TestFinalizeReadWarmCacheRetainsRejectedInput(t *testing.T) {
	cfg, st := setup(t)
	writeNote(t, cfg, 1, "old healthy", note.Metadata{Title: "Healthy"})
	writeNote(t, cfg, 2, "old cached [[Healthy]]", note.Metadata{Title: "Cached", Tags: []string{"retained"}})
	ix := New(cfg, st)
	seed, err := ix.Stage(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ix.FinalizeRead(context.Background(), seed); err != nil {
		t.Fatal(err)
	}
	if err := ix.Publish(seed); err != nil {
		t.Fatal(err)
	}
	cached, err := st.SearchByID(2)
	if err != nil || len(cached) != 1 {
		t.Fatal(cached, err)
	}
	writeNote(t, cfg, 1, "new healthy", note.Metadata{Title: "Updated healthy"})
	snap, err := ix.Stage(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.NotePath(2), []byte("changed after staging, not verified"), 0600); err != nil {
		t.Fatal(err)
	}
	before := canonicalInventory(t, cfg)
	if err := ix.FinalizeRead(context.Background(), snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.Notes) != 1 || snap.Notes[0].ID != 1 || !snap.State.Partial || !snap.State.CacheUsed {
		t.Fatalf("unexpected finalized subset: %+v", snap)
	}
	if len(snap.State.Issues) != 1 || snap.State.Issues[0].NoteID != 2 || !snap.State.Issues[0].Cached {
		t.Fatalf("missing cached diagnostic: %+v", snap.State)
	}
	if err := ix.Publish(snap); err != nil {
		t.Fatal(err)
	}
	retained, err := st.SearchByID(2)
	if err != nil || !reflect.DeepEqual(cached, retained) {
		t.Fatalf("cached representation changed: %+v %+v %v", cached, retained, err)
	}
	updated, err := st.SearchByID(1)
	if err != nil || len(updated) != 1 || updated[0].Title != "Updated healthy" {
		t.Fatal(updated, err)
	}
	state, err := st.ReadState()
	if err != nil || !reflect.DeepEqual(state, snap.State) {
		t.Fatal(state, err)
	}
	if !reflect.DeepEqual(before, canonicalInventory(t, cfg)) {
		t.Fatal("canonical files changed")
	}
}

package index

import (
	"context"
	"errors"
	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
	"github.com/ttak0422/track/internal/track/vaultfile"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestStagePublishesHealthyProgressBeforeBlockedItemReturns(t *testing.T) {
	cfg, s := setup(t)
	writeNote(t, cfg, 1, "healthy body", note.Metadata{Title: "Healthy"})
	writeNote(t, cfg, 2, "blocked body", note.Metadata{Title: "Blocked"})
	ix := New(cfg, s)
	realRead := ix.readNote
	blocked := make(chan struct{})
	progress := make(chan *note.Note, 2)
	ix.readNote = func(ctx context.Context, path string, cfg *config.Config) (*note.Note, error) {
		if filepath.Base(path) == "2.md" {
			close(blocked)
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return realRead(ctx, path, cfg)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan *Snapshot, 1)
	go func() {
		snap, err := ix.Stage(ctx, func(n *note.Note) { progress <- n })
		if err != nil {
			t.Error(err)
		}
		done <- snap
	}()
	<-blocked
	select {
	case n := <-progress:
		if n.ID != 1 {
			t.Fatal(n.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("healthy cold item waited for blocked peer")
	}
	cancel()
	snap := <-done
	if snap == nil || !snap.State.Partial || len(snap.Notes) != 1 {
		t.Fatalf("partial snapshot: %+v", snap)
	}
	if err := ix.Publish(snap); err != nil {
		t.Fatal(err)
	}
	refs, err := s.AllNotes()
	if err != nil || len(refs) != 1 {
		t.Fatal(refs, err)
	}
}
func TestStagingDiagnosticsDoNotSupersedeButMutationDoes(t *testing.T) {
	cfg, s := setup(t)
	writeNote(t, cfg, 1, "body", note.Metadata{Title: "Keep"})
	ix := New(cfg, s)
	snap, err := ix.Stage(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddReadIssue(store.ReadIssue{Path: "synthetic", Reason: "optional query unavailable"}); err != nil {
		t.Fatal(err)
	}
	if err := ix.Publish(snap); err != nil {
		t.Fatalf("diagnostic blocked healthy publication: %v", err)
	}
	snap, err = ix.Stage(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.InvalidateSnapshot(); err != nil {
		t.Fatal(err)
	}
	if err := ix.Publish(snap); !errors.Is(err, ErrSuperseded) {
		t.Fatalf("late generation published: %v", err)
	}
}
func TestCommitIsolatesAFileChangedAfterStaging(t *testing.T) {
	cfg, s := setup(t)
	writeNote(t, cfg, 1, "old A", note.Metadata{Title: "A"})
	writeNote(t, cfg, 2, "old B [[A]]", note.Metadata{Title: "B", Tags: []string{"keep"}})
	ix := New(cfg, s)
	if _, err := ix.Full(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.NotePath(1), []byte("new verified A"), 0600); err != nil {
		t.Fatal(err)
	}
	snap, err := ix.Stage(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(cfg.NotePath(2), cfg.NotePath(2)+".held"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := ix.ApplyActivity(ctx, snap); err != nil {
		t.Fatal(err)
	}
	if !snap.State.Partial || len(snap.Notes) != 1 || snap.Notes[0].ID != 1 {
		t.Fatalf("changed B discarded A: %+v", snap.State)
	}
	if err := ix.Publish(snap); err != nil {
		t.Fatal(err)
	}
	refs, err := s.SearchByID(2)
	if err != nil || len(refs) != 1 || len(refs[0].Tags) != 1 {
		t.Fatalf("B cache lost: %+v %v", refs, err)
	}
	links, err := s.Backlinks(1)
	if err != nil || len(links) != 1 {
		t.Fatalf("B links lost: %+v %v", links, err)
	}
}
func TestInitializationLockHasCallerDeadline(t *testing.T) {
	cfg, s := setup(t)
	lock, err := os.OpenFile(cfg.DBPath+".init.lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New(cfg, s).EnsureInitializedContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("lock wait ignored cancellation: %v", err)
	}
	initialized, err := s.Initialized()
	if err != nil || initialized {
		t.Fatalf("incomplete lock initialized cache: %v %v", initialized, err)
	}
}

func TestStageReusesOnlyVerifiedUnchangedFingerprints(t *testing.T) {
	cfg, st := setup(t)
	writeNote(t, cfg, 1, "initial", note.Metadata{Title: "One"})
	ix := New(cfg, st)
	first, err := ix.Stage(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ix.Publish(first); err != nil {
		t.Fatal(err)
	}
	previous := map[int64]*vaultfile.NoteRead{1: vaultfile.VersionOnly(first.Notes[0].Snapshot)}
	next := New(cfg, st).WithPrevious(previous)
	realRead := next.readNote
	reads := 0
	next.readNote = func(ctx context.Context, path string, cfg *config.Config) (*note.Note, error) {
		reads++
		return realRead(ctx, path, cfg)
	}
	unchanged, err := next.Stage(context.Background(), nil)
	if err != nil || unchanged.State.Partial || len(unchanged.Notes) != 0 || reads != 0 {
		t.Fatalf("unchanged body reloaded: %+v %v %d", unchanged, err, reads)
	}
	if err := os.WriteFile(cfg.NotePath(1), []byte("external replacement with a different length"), 0600); err != nil {
		t.Fatal(err)
	}
	changed, err := next.Stage(context.Background(), nil)
	if err != nil || len(changed.Notes) != 1 || reads != 1 {
		t.Fatalf("changed input skipped: %+v %v %d", changed, err, reads)
	}
	if err := os.Remove(cfg.NotePath(1)); err != nil {
		t.Fatal(err)
	}
	missing, err := next.Stage(context.Background(), nil)
	if err != nil || !missing.State.Partial {
		t.Fatal(missing, err)
	}
	if err := next.Publish(missing); err != nil {
		t.Fatal(err)
	}
	refs, err := st.AllNotes()
	if err != nil || len(refs) != 1 {
		t.Fatal("missing input pruned", refs, err)
	}
}

func TestUnverifiedStagesNeverUpdateActivitySidecars(t *testing.T) {
	for _, mode := range []string{"partial", "provisional"} {
		t.Run(mode, func(t *testing.T) {
			cfg, st := setup(t)
			writeNote(t, cfg, 1, "readable", note.Metadata{Title: "Keep metadata"})
			ix := New(cfg, st)
			snap, err := ix.Stage(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			snap.Activity[1] = snap.Notes[0].Mtime
			if mode == "partial" {
				snap.State.Partial = true
			} else {
				cfg.Provisional = true
			}
			before, err := os.ReadFile(cfg.MetadataPath(1))
			if err != nil {
				t.Fatal(err)
			}
			if err := ix.ApplyActivity(context.Background(), snap); err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(cfg.MetadataPath(1))
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) {
				t.Fatal("unverified stage changed metadata")
			}
			if len(snap.Notes) != 1 {
				t.Fatal("healthy read was discarded")
			}
		})
	}
}

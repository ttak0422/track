package index

import (
	"bytes"
	"os"
	"sync"
	"testing"

	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

func TestEnsureInitializedFinishesPartialCacheWithoutChangingMetadata(t *testing.T) {
	cfg, s := setup(t)
	writeNote(t, cfg, 1, "Parent", note.Metadata{Title: "Parent", Days: []string{"2020-01-01"}})
	writeNote(t, cfg, 2, "[[Parent]]", note.Metadata{Title: "Child"})
	before := map[int64][]byte{}
	// Simulate interruption after every note row was written, but before links
	// and the completed-build marker. Matching mtimes alone must not hide this.
	for _, id := range []int64{1, 2} {
		var err error
		before[id], err = os.ReadFile(cfg.MetadataPath(id))
		if err != nil {
			t.Fatal(err)
		}
		n, err := note.ParseFile(cfg.NotePath(id), cfg)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.UpsertNote(n); err != nil {
			t.Fatal(err)
		}
	}
	if initialized, err := s.Initialized(); err != nil || initialized {
		t.Fatalf("partial initialization = %v, %v", initialized, err)
	}
	if changed, err := New(cfg, s).RefreshIfStale(); err != nil || !changed {
		t.Fatalf("recover partial cache = %v, %v", changed, err)
	}
	if links, err := s.Backlinks(1); err != nil || len(links) != 1 || links[0].NoteID != 2 {
		t.Fatalf("recovered links = %+v, %v", links, err)
	}
	for id, want := range before {
		got, err := os.ReadFile(cfg.MetadataPath(id))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("bootstrap changed historical metadata %d: %q, %v", id, got, err)
		}
	}
	if changed, err := New(cfg, s).EnsureInitialized(); err != nil || changed {
		t.Fatalf("second initialization = %v, %v", changed, err)
	}
}

func TestEnsureInitializedSerializesColdBuilders(t *testing.T) {
	cfg, s := setup(t)
	writeNote(t, cfg, 1, "body", note.Metadata{Title: "Existing"})
	const workers = 8
	stores := make([]*store.Store, workers)
	for i := range stores {
		var err error
		stores[i], err = store.Open(cfg.DBPath)
		if err != nil {
			t.Fatal(err)
		}
		defer stores[i].Close()
	}
	start := make(chan struct{})
	results := make(chan bool, workers)
	errors := make(chan error, workers)
	var wg sync.WaitGroup
	for _, st := range stores {
		wg.Go(func() {
			<-start
			changed, err := New(cfg, st).EnsureInitialized()
			results <- changed
			errors <- err
		})
	}
	close(start)
	wg.Wait()
	close(results)
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	builds := 0
	for changed := range results {
		if changed {
			builds++
		}
	}
	if builds != 1 {
		t.Fatalf("cold builds = %d, want exactly one", builds)
	}
	if _, found, err := s.ResolveTerm("Existing"); err != nil || !found {
		t.Fatalf("initialized title missing: %v, %v", found, err)
	}
}

func TestEnsureInitializedEmptyVaultAndFailure(t *testing.T) {
	t.Run("empty vault is initialized once", func(t *testing.T) {
		cfg, s := setup(t)
		ix := New(cfg, s)
		if changed, err := ix.EnsureInitialized(); err != nil || !changed {
			t.Fatalf("first = %v, %v", changed, err)
		}
		if changed, err := ix.EnsureInitialized(); err != nil || changed {
			t.Fatalf("second = %v, %v", changed, err)
		}
	})
	t.Run("failed build is not marked complete", func(t *testing.T) {
		cfg, s := setup(t)
		if err := os.WriteFile(cfg.NoteDir(), []byte("not a directory"), 0o600); err != nil {
			t.Fatal(err)
		}
		if changed, err := New(cfg, s).EnsureInitialized(); err == nil || changed {
			t.Fatalf("failed build = %v, %v", changed, err)
		}
		if initialized, err := s.Initialized(); err != nil || initialized {
			t.Fatalf("failed marker = %v, %v", initialized, err)
		}
	})
}

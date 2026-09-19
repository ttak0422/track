package provenance

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/gen"
	"github.com/ttak0422/track/internal/track/note"
)

func evidenceNote(t *testing.T, cfg *config.Config, id int64, body string) {
	t.Helper()
	if err := os.MkdirAll(cfg.NoteDir(), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.NotePath(id), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	if err := note.WriteMetadata(cfg.MetadataPath(id), note.Metadata{Title: "Evidence"}); err != nil {
		t.Fatal(err)
	}
}

func TestSourceAndDerivedVersions(t *testing.T) {
	cfg := &config.Config{VaultDir: t.TempDir(), Extensions: []string{".md"}, GenKeep: 1}
	evidenceNote(t, cfg, 100, "## Findings\nOriginal evidence\n")
	original := filepath.Join(t.TempDir(), "original.pdf")
	if err := os.WriteFile(original, []byte("PDF bytes\x00"), 0644); err != nil {
		t.Fatal(err)
	}
	opts := Options{Source: "https://example.test/article", Format: "application/pdf", At: "2026-09-19T09:00:00+09:00", Original: original}
	first, created, err := Save(cfg, 100, opts)
	if err != nil || !created || first.RecordedAt != "2026-09-19T00:00:00Z" {
		t.Fatalf("save: %+v %v %v", first, created, err)
	}
	opts.At = "2026-09-20T00:00:00Z"
	same, created, err := Save(cfg, 100, opts)
	if err != nil || created || same.Version != first.Version || same.RecordedAt != first.RecordedAt {
		t.Fatalf("retry: %+v %v %v", same, created, err)
	}
	evidenceNote(t, cfg, 100, "Changed evidence\n")
	second, created, err := Save(cfg, 100, opts)
	if err != nil || !created || second.Version == first.Version {
		t.Fatalf("changed: %+v %v %v", second, created, err)
	}
	if err := os.WriteFile(original, []byte("corrected PDF"), 0644); err != nil {
		t.Fatal(err)
	}
	third, created, err := Save(cfg, 100, opts)
	if err != nil || !created || third.Version == second.Version {
		t.Fatalf("changed original: %+v %v %v", third, created, err)
	}
	if err := os.Remove(original); err != nil {
		t.Fatal(err)
	}
	if err := note.WriteMetadata(cfg.MetadataPath(100), note.Metadata{Title: "Renamed"}); err != nil {
		t.Fatal(err)
	}
	old, err := Read(cfg, first.Reference)
	if err != nil || old.Body != "## Findings\nOriginal evidence\n" {
		t.Fatalf("old version: %+v %v", old, err)
	}
	evidenceNote(t, cfg, 200, "Summary A\n")
	derivedOpts := Options{Inputs: []Reference{first.Reference}, Method: "summarizer/model-v1", Settings: "prompt-v1", Format: "text/markdown", At: opts.At}
	derived, created, err := Save(cfg, 200, derivedOpts)
	if err != nil || !created {
		t.Fatalf("derive: %+v %v %v", derived, created, err)
	}
	evidenceNote(t, cfg, 200, "Summary B (nondeterministic retry)\n")
	retry, created, err := Save(cfg, 200, derivedOpts)
	if err != nil || created || retry.Body != derived.Body {
		t.Fatalf("derive retry: %+v %v %v", retry, created, err)
	}
	derivedOpts.Settings = "prompt-v2"
	changed, created, err := Save(cfg, 200, derivedOpts)
	if err != nil || !created || changed.Version == derived.Version {
		t.Fatalf("settings: %+v %v %v", changed, created, err)
	}
	derivedOpts.Run = "explicit-run-1"
	regenerated, created, err := Save(cfg, 200, derivedOpts)
	if err != nil || !created || regenerated.Version == changed.Version {
		t.Fatalf("regeneration: %+v %v %v", regenerated, created, err)
	}
	_, created, err = Save(cfg, 200, derivedOpts)
	if err != nil || created {
		t.Fatalf("regeneration retry: %v %v", created, err)
	}
	// Vault generations, including pruning, cannot rewrite saved source versions.
	g := gen.New(cfg)
	if _, err := g.Increment(""); err != nil {
		t.Fatal(err)
	}
	evidenceNote(t, cfg, 100, "latest working body")
	if _, err := g.Increment(""); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(cfg, first.Reference); err != nil {
		t.Fatal(err)
	}
	records, err := List(cfg, 200)
	if err != nil || len(records) != 3 {
		t.Fatalf("list: %+v %v", records, err)
	}
	// A missing input cannot produce a successful derived record.
	if err := os.Remove(cfg.NotePath(100)); err != nil {
		t.Fatal(err)
	}
	derivedOpts.Run = "missing-input"
	if _, _, err := Save(cfg, 200, derivedOpts); err == nil {
		t.Fatal("saved missing input")
	}
	if _, err := Read(cfg, first.Reference); err == nil {
		t.Fatal("resolved deleted note")
	}
}

func TestSaveFailureRetryAndIntegrity(t *testing.T) {
	cfg := &config.Config{VaultDir: t.TempDir(), Extensions: []string{".md"}}
	evidenceNote(t, cfg, 100, "evidence")
	opts := Options{Source: "https://example.test", Format: "text/plain", At: "2026-09-19T00:00:00Z"}
	dir := filepath.Join(cfg.TrackDir(), "sources")
	if err := os.WriteFile(dir, []byte("obstruction"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Save(cfg, 100, opts); err == nil {
		t.Fatal("save succeeded despite write failure")
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	r, created, err := Save(cfg, 100, opts)
	if err != nil || !created {
		t.Fatalf("retry: %+v %v %v", r, created, err)
	}
	// Interrupted stages are invisible and do not block a retry.
	stage := filepath.Join(dir, "100", ".pending-interrupted")
	if err := os.Mkdir(stage, 0700); err != nil {
		t.Fatal(err)
	}
	list, err := List(cfg, 100)
	if err != nil || len(list) != 1 {
		t.Fatalf("partial stage published: %+v %v", list, err)
	}
	recordPath, _ := recordDir(cfg, r.Reference)
	if err := os.WriteFile(filepath.Join(recordPath, "record.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(cfg, r.Reference); err == nil {
		t.Fatal("read corrupt record")
	}
	if _, _, err := Save(cfg, 100, opts); err == nil {
		t.Fatal("silently replaced corrupt version")
	}
	for _, ref := range []Reference{{NoteID: 100, Version: "../../elsewhere"}, {NoteID: -1, Version: r.Version}} {
		if _, err := Read(cfg, ref); err == nil {
			t.Fatal("accepted invalid reference")
		}
	}
	opts.At = "2026-09-19"
	if _, _, err := Save(cfg, 100, opts); err == nil {
		t.Fatal("inferred timezone")
	}
}

func TestConcurrentSourceSave(t *testing.T) {
	cfg := &config.Config{VaultDir: t.TempDir(), Extensions: []string{".md"}}
	evidenceNote(t, cfg, 100, "evidence")
	opts := Options{Source: "https://example.test", Format: "text/plain", At: "2026-09-19T00:00:00Z"}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, _, err := Save(cfg, 100, opts); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	list, err := List(cfg, 100)
	if err != nil || len(list) != 1 {
		t.Fatalf("concurrent save: %+v %v", list, err)
	}
}

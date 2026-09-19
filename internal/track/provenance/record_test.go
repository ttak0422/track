package provenance

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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

func TestSaveReusesAcrossNotes(t *testing.T) {
	cfg := &config.Config{VaultDir: t.TempDir(), Extensions: []string{".md"}}
	evidenceNote(t, cfg, 100, "evidence")
	evidenceNote(t, cfg, 200, "evidence")
	opts := Options{Source: "https://example.test", Format: "text/plain", At: "2026-09-19T00:00:00Z"}
	first, _, err := Save(cfg, 100, opts)
	if err != nil {
		t.Fatal(err)
	}
	opts.At = "2026-09-20T00:00:00Z"
	again, created, err := Save(cfg, 200, opts)
	if err != nil || created || again.Reference != first.Reference || again.RecordedAt != first.RecordedAt {
		t.Fatalf("cross-note retry: %+v %v %v", again, created, err)
	}
	if records, err := List(cfg, 200); err != nil || len(records) != 0 {
		t.Fatalf("duplicate owns versions: %+v %v", records, err)
	}
	if body, err := os.ReadFile(cfg.NotePath(200)); err != nil || string(body) != "evidence" {
		t.Fatalf("working duplicate changed: %q %v", body, err)
	}
	// Inputs remain exact: inventing a reference using the duplicate working note must fail.
	derived := Options{Inputs: []Reference{{NoteID: 200, Version: first.Version}}, Method: "model", Settings: "prompt", Format: "text/plain", At: opts.At}
	evidenceNote(t, cfg, 300, "summary")
	if _, _, err := Save(cfg, 300, derived); err == nil {
		t.Fatal("accepted nonexistent duplicate input reference")
	}
	derived.Inputs = []Reference{again.Reference}
	summary, _, err := Save(cfg, 300, derived)
	if err != nil {
		t.Fatal(err)
	}
	evidenceNote(t, cfg, 400, "another nondeterministic output")
	retry, created, err := Save(cfg, 400, derived)
	if err != nil || created || retry.Reference != summary.Reference || retry.Body != summary.Body {
		t.Fatalf("derived retry: %+v %v %v", retry, created, err)
	}
	derived.Run = "another-run"
	if _, created, err := Save(cfg, 400, derived); err != nil || !created {
		t.Fatalf("explicit regeneration: %v %v", created, err)
	}
	// Identity fields still separate sources even when their working body is identical.
	for _, changed := range []Options{
		{Source: "https://example.test/other", Format: opts.Format, At: opts.At},
		{Source: opts.Source, Format: "text/markdown", At: opts.At},
	} {
		if _, created, err := Save(cfg, 200, changed); err != nil || !created {
			t.Fatalf("changed identity: %v %v", created, err)
		}
	}
}

func TestSaveRejectsBrokenDuplicate(t *testing.T) {
	for _, damage := range []string{"deleted owner", "missing record", "corrupt record", "corrupt original"} {
		t.Run(damage, func(t *testing.T) {
			cfg := &config.Config{VaultDir: t.TempDir(), Extensions: []string{".md"}}
			evidenceNote(t, cfg, 100, "evidence")
			evidenceNote(t, cfg, 200, "evidence")
			original := filepath.Join(t.TempDir(), "original")
			if err := os.WriteFile(original, []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			opts := Options{Source: "https://example.test", Format: "text/plain", At: "2026-09-19T00:00:00Z", Original: original}
			r, _, err := Save(cfg, 100, opts)
			if err != nil {
				t.Fatal(err)
			}
			dir, _ := recordDir(cfg, r.Reference)
			switch damage {
			case "deleted owner":
				err = os.Remove(cfg.NotePath(100))
			case "missing record":
				err = os.Remove(filepath.Join(dir, "record.json"))
			case "corrupt record":
				err = os.WriteFile(filepath.Join(dir, "record.json"), []byte("{}"), 0600)
			case "corrupt original":
				err = os.WriteFile(filepath.Join(dir, "original"), []byte("changed"), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := Save(cfg, 200, opts); err == nil {
				t.Fatal("silently retargeted broken duplicate")
			}
		})
	}
}

func TestSavePreservesLegacyDuplicateReferences(t *testing.T) {
	cfg := &config.Config{VaultDir: t.TempDir(), Extensions: []string{".md"}}
	evidenceNote(t, cfg, 100, "evidence")
	evidenceNote(t, cfg, 200, "evidence")
	opts := Options{Source: "https://example.test", Format: "text/plain", At: "2026-09-19T00:00:00Z"}
	r, _, err := Save(cfg, 100, opts)
	if err != nil {
		t.Fatal(err)
	}
	// Reproduce a record stored under a second owner before cross-note deduplication.
	legacy := r
	legacy.NoteID = 200
	dir, _ := recordDir(cfg, legacy.Reference)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "record.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	again, created, err := Save(cfg, 200, opts)
	if err != nil || created || again.Reference != r.Reference {
		t.Fatalf("legacy retry: %+v %v %v", again, created, err)
	}
	if read, err := Read(cfg, legacy.Reference); err != nil || read.Reference != legacy.Reference {
		t.Fatalf("legacy citation changed: %+v %v", read, err)
	}
	if err := os.Remove(cfg.NotePath(200)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Save(cfg, 100, opts); err == nil {
		t.Fatal("healthy owner hid broken legacy duplicate")
	}
}

func TestConcurrentSourceSaveProcesses(t *testing.T) {
	opts := Options{Source: "https://example.test", Format: "text/plain", At: "2026-09-19T00:00:00Z"}
	if vault := os.Getenv("TRACK_SOURCE_TEST_VAULT"); vault != "" {
		id, err := strconv.ParseInt(os.Getenv("TRACK_SOURCE_TEST_ID"), 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		r, created, err := Save(&config.Config{VaultDir: vault, Extensions: []string{".md"}}, id, opts)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.NewEncoder(os.Stdout).Encode(struct {
			Reference
			Created bool
		}{r.Reference, created}); err != nil {
			t.Fatal(err)
		}
		os.Exit(0)
	}
	cfg := &config.Config{VaultDir: t.TempDir(), Extensions: []string{".md"}}
	for id := int64(100); id < 108; id++ {
		evidenceNote(t, cfg, id, "evidence")
	}
	results := make(chan struct {
		Reference
		Created bool
	}, 8)
	var wg sync.WaitGroup
	for id := int64(100); id < 108; id++ {
		wg.Go(func() {
			cmd := exec.Command(os.Args[0], "-test.run=^TestConcurrentSourceSaveProcesses$")
			cmd.Env = append(os.Environ(), "TRACK_SOURCE_TEST_VAULT="+cfg.VaultDir, "TRACK_SOURCE_TEST_ID="+strconv.FormatInt(id, 10))
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Errorf("save subprocess: %s: %v", out, err)
				return
			}
			var result struct {
				Reference
				Created bool
			}
			if err := json.Unmarshal(out, &result); err != nil {
				t.Errorf("decode subprocess: %s: %v", out, err)
				return
			}
			results <- result
		})
	}
	wg.Wait()
	close(results)
	created, returned := 0, 0
	var canonical Reference
	for result := range results {
		if canonical.NoteID == 0 {
			canonical = result.Reference
		}
		if result.Reference != canonical {
			t.Fatalf("different canonical references: %+v %+v", canonical, result.Reference)
		}
		if result.Created {
			created++
		}
		returned++
	}
	if created != 1 || returned != 8 {
		t.Fatalf("created %d versions across %d results", created, returned)
	}
}

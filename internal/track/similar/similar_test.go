package similar

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

// TestCommandEmbedderTimesOut proves a hung embedder cannot hang the caller: the command is killed at
// embedTimeout and the error says so.
func TestCommandEmbedderTimesOut(t *testing.T) {
	old := embedTimeout
	embedTimeout = 200 * time.Millisecond
	t.Cleanup(func() { embedTimeout = old })

	script := filepath.Join(t.TempDir(), "slow-embed.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexec sleep 5\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	embed, ok := CommandEmbedder(&config.Config{EmbedderCommand: []string{script}})
	if !ok {
		t.Fatal("expected an embedder")
	}

	start := time.Now()
	_, err := embed("some text")
	elapsed := time.Since(start)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("want a timeout error, got %v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout error must wrap context.DeadlineExceeded, got %v", err)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("embed returned after %s; the timeout did not bound the call", elapsed)
	}
}

// fakeEmbed is a deterministic stand-in for a real model: it never shells out. It maps text to a tiny
// 4-dim vector by counting a few marker letters, so notes with similar letter mixes score close. Enough
// to exercise cosine ranking and the cache without touching a model.
func fakeEmbed(text string) ([]float32, error) {
	var v [4]float32
	for _, r := range text {
		switch r {
		case 'a', 'A':
			v[0]++
		case 'e', 'E':
			v[1]++
		case 'i', 'I':
			v[2]++
		case 'o', 'O':
			v[3]++
		}
	}
	return v[:], nil
}

func TestCosine(t *testing.T) {
	cases := []struct {
		a, b []float32
		want float64
	}{
		{[]float32{1, 0}, []float32{1, 0}, 1},
		{[]float32{1, 0}, []float32{0, 1}, 0},
		{[]float32{1, 0}, []float32{-1, 0}, -1},
		{[]float32{0, 0}, []float32{1, 1}, 0},    // zero vector
		{[]float32{1, 2, 3}, []float32{1, 2}, 0}, // dimension mismatch never panics
	}
	for _, c := range cases {
		if got := cosine(c.a, c.b); got != c.want {
			t.Fatalf("cosine(%v,%v)=%v want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestNearestRanksAndTrims(t *testing.T) {
	all := []store.Embedding{
		{NoteID: 1, FileKind: "note", Title: "target", Vector: []float32{1, 0, 0}},
		{NoteID: 2, FileKind: "note", Title: "same-direction", Vector: []float32{2, 0, 0}},
		{NoteID: 3, FileKind: "note", Title: "orthogonal", Vector: []float32{0, 1, 0}},
		{NoteID: 4, FileKind: "note", Title: "near", Vector: []float32{1, 0.1, 0}},
	}
	res, err := Nearest(all, 1, 2)
	if err != nil {
		t.Fatalf("nearest: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("limit not honored: %d results", len(res))
	}
	// note 2 points the exact same direction as the target (score 1); note 4 is close; note 3 is
	// orthogonal and must be dropped by the limit.
	if res[0].NoteID != 2 || res[1].NoteID != 4 {
		t.Fatalf("unexpected ranking: %+v", res)
	}
	if res[0].Score <= res[1].Score {
		t.Fatalf("results must be sorted by descending score: %+v", res)
	}
}

func TestNearestMissingTarget(t *testing.T) {
	if _, err := Nearest(nil, 99, 5); err == nil {
		t.Fatal("expected an error when the target has no embedding")
	}
}

// TestEnsureSkipsUnchanged drives the real cache path against a real store: the first Ensure embeds every
// note, a second embeds nothing, and editing one note re-embeds only that note.
func TestEnsureSkipsUnchanged(t *testing.T) {
	vault := t.TempDir()
	cfg := &config.Config{
		VaultDir:        vault,
		Extensions:      []string{".md"},
		DateFormat:      "2006-01-02",
		EmbedderCommand: []string{"fake"}, // only its signature is used; embedding goes through the injected func
	}
	if _, err := cfg.EnsureVaultSkeleton(); err != nil {
		t.Fatal(err)
	}

	writeNote(t, cfg, 1, "Alpha", "aaa eee")
	writeNote(t, cfg, 2, "Beta", "ooo iii")

	s, err := store.Open(filepath.Join(vault, "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := index.New(cfg, s).Full(); err != nil {
		t.Fatal(err)
	}

	calls := 0
	counting := func(text string) ([]float32, error) {
		calls++
		return fakeEmbed(text)
	}

	n, err := Ensure(cfg, s, counting)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if n != 2 || calls != 2 {
		t.Fatalf("first ensure: embedded %d, %d calls; want 2 and 2", n, calls)
	}

	// Nothing changed: no re-embedding.
	calls = 0
	n, err = Ensure(cfg, s, counting)
	if err != nil {
		t.Fatalf("ensure again: %v", err)
	}
	if n != 0 || calls != 0 {
		t.Fatalf("unchanged ensure: embedded %d, %d calls; want 0 and 0", n, calls)
	}

	// Change one note's body; only that note is re-embedded.
	writeNote(t, cfg, 1, "Alpha", "iii ooo different")
	if _, err := index.New(cfg, s).Full(); err != nil {
		t.Fatal(err)
	}
	calls = 0
	n, err = Ensure(cfg, s, counting)
	if err != nil {
		t.Fatalf("ensure after edit: %v", err)
	}
	if n != 1 || calls != 1 {
		t.Fatalf("edited ensure: embedded %d, %d calls; want 1 and 1", n, calls)
	}

	// Changing the embedder signature invalidates every cached vector.
	cfg.EmbedderCommand = []string{"other-model"}
	calls = 0
	n, err = Ensure(cfg, s, counting)
	if err != nil {
		t.Fatalf("ensure after embedder swap: %v", err)
	}
	if n != 2 || calls != 2 {
		t.Fatalf("embedder-swap ensure: embedded %d, %d calls; want 2 and 2", n, calls)
	}

	// A different split of the same words is a different command and must also re-embed: argv
	// elements may contain spaces, so a space-joined signature would collide here.
	cfg.EmbedderCommand = []string{"other", "model"}
	calls = 0
	n, err = Ensure(cfg, s, counting)
	if err != nil {
		t.Fatalf("ensure after argv resplit: %v", err)
	}
	if n != 2 || calls != 2 {
		t.Fatalf("argv-resplit ensure: embedded %d, %d calls; want 2 and 2", n, calls)
	}
}

func writeNote(t *testing.T, cfg *config.Config, id int64, title, body string) {
	t.Helper()
	if err := os.WriteFile(cfg.NotePath(id), []byte(body+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	meta := note.Metadata{Version: note.CurrentMetadataVersion, Title: title, Created: "2026-01-01"}
	if err := note.WriteMetadata(cfg.MetadataPath(id), meta); err != nil {
		t.Fatal(err)
	}
}

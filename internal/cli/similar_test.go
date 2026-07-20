package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeEmbedderScript writes a deterministic, model-free embedder: it reads note text on stdin and prints
// a 4-dim JSON vector counting the letters a/e/i/o. Notes with similar letter mixes point the same way,
// which is all the similarity ranking needs. It never calls a real model.
func fakeEmbedderScript(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-embed.sh")
	script := `#!/bin/sh
awk '{ t = t $0 "\n" } END {
  a = gsub(/[aA]/, "", t)
  e = gsub(/[eE]/, "", t)
  i = gsub(/[iI]/, "", t)
  o = gsub(/[oO]/, "", t)
  printf("[%d,%d,%d,%d]\n", a, e, i, o)
}'
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSimilarWithEmbedder(t *testing.T) {
	vault := t.TempDir()
	t.Setenv("TRACK_EMBEDDER", fakeEmbedderScript(t))

	// Bodies chosen so A and B share a letter mix (a/e) while C is on a different axis (i/o).
	target, code := runIn(t, vault, "new", "--title", "A", "--id", "1000", "--body", "aaa eee")
	if code != 0 {
		t.Fatalf("new target: %v", target)
	}
	if _, code := runIn(t, vault, "new", "--title", "B", "--id", "1001", "--body", "aaaa eeee"); code != 0 {
		t.Fatal("new B failed")
	}
	if _, code := runIn(t, vault, "new", "--title", "C", "--id", "1002", "--body", "ooo iii"); code != 0 {
		t.Fatal("new C failed")
	}

	out, code := runIn(t, vault, "similar", "--id", "1000", "--limit", "5")
	if code != 0 {
		t.Fatalf("similar failed: %v", out)
	}
	if out["embedder"] != true {
		t.Fatalf("expected embedder=true, got %v", out)
	}
	results, ok := out["results"].([]any)
	if !ok || len(results) != 2 {
		t.Fatalf("expected 2 related notes (target excluded), got %v", out["results"])
	}
	first := results[0].(map[string]any)
	if first["note_id"].(float64) != 1001 {
		t.Fatalf("closest note should be B (1001), got %v", first)
	}
	if first["score"].(float64) <= results[1].(map[string]any)["score"].(float64) {
		t.Fatalf("results must be sorted by descending score: %v", results)
	}
	if first["path"] == "" || first["title"] != "B" {
		t.Fatalf("result missing path/title: %v", first)
	}

	// A second run re-embeds nothing (unchanged content hashes) yet returns the same ranking.
	again, code := runIn(t, vault, "similar", "--id", "1000")
	if code != 0 || len(again["results"].([]any)) != 2 {
		t.Fatalf("second similar diverged: code=%d out=%v", code, again)
	}
}

func TestSimilarWithoutEmbedder(t *testing.T) {
	vault := t.TempDir()
	t.Setenv("TRACK_EMBEDDER", "") // no embedder configured

	if _, code := runIn(t, vault, "new", "--title", "Solo", "--id", "2000"); code != 0 {
		t.Fatal("new failed")
	}

	out, code := runIn(t, vault, "similar", "--id", "2000")
	// Exits cleanly (0) and explains how to configure an embedder rather than erroring.
	if code != 0 {
		t.Fatalf("expected clean exit without an embedder, got code=%d out=%v", code, out)
	}
	if out["embedder"] != false {
		t.Fatalf("expected embedder=false, got %v", out)
	}
	if msg, _ := out["message"].(string); msg == "" {
		t.Fatalf("expected a setup message, got %v", out)
	}
}

func TestSimilarRequiresID(t *testing.T) {
	out, code := runIn(t, t.TempDir(), "similar")
	if code != 1 || out["error"] == nil {
		t.Fatalf("expected --id required error, got code=%d out=%v", code, out)
	}
}

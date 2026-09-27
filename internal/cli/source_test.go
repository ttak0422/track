package cli

import (
	"testing"
)

func TestSourceCLIRebuildAndRetry(t *testing.T) {
	vault := t.TempDir()
	run := func(args ...string) map[string]any {
		t.Helper()
		out, code := runIn(t, vault, args...)
		if code != 0 {
			t.Fatalf("%v: %v", args, out)
		}
		return out
	}
	run("new", "--id", "100", "--title", "Source", "--body", "Evidence\n")
	args := []string{"source", "save", "--id", "100", "--source", "https://example.test/document", "--format", "text/html", "--at", "2026-09-19T00:00:00Z"}
	saved := run(args...)
	version := saved["record"].(map[string]any)["version"].(string)
	if saved["created"] != true {
		t.Fatal(saved)
	}
	run("reindex", "--full")
	again := run(args...)
	if again["created"] != false || again["record"].(map[string]any)["version"] != version {
		t.Fatal(again)
	}
	run("new", "--id", "101", "--title", "Duplicate source", "--body", "Evidence\n")
	duplicateArgs := append([]string(nil), args...)
	duplicateArgs[3] = "101"
	duplicate := run(duplicateArgs...)
	canonical := duplicate["record"].(map[string]any)
	if duplicate["created"] != false || canonical["note_id"] != float64(100) || canonical["version"] != version {
		t.Fatal(duplicate)
	}
	if len(run("source", "list", "--id", "101")["versions"].([]any)) != 0 {
		t.Fatal("duplicate version stored")
	}
	citation := run("cite", "--id", "100", "--version", canonical["version"].(string))
	if citation["body"] != "Evidence\n" {
		t.Fatal(citation)
	}
	run("new", "--id", "200", "--title", "Summary", "--body", "A summary")
	derived := run("source", "save", "--id", "200", "--input", "100:"+version, "--method", "model-v1", "--settings", "prompt-v1", "--format", "text/markdown", "--at", "2026-09-19T01:00:00Z")
	if derived["record"].(map[string]any)["kind"] != "derived" {
		t.Fatal(derived)
	}
	run("reindex", "--full")
	listed := run("source", "list", "--id", "200")
	if len(listed["versions"].([]any)) != 1 {
		t.Fatal(listed)
	}
	// Old notes remain ordinary readable notes, with no required provenance migration.
	run("new", "--id", "300", "--title", "Legacy", "--body", "No provenance")
	if len(run("source", "list", "--id", "300")["versions"].([]any)) != 0 {
		t.Fatal("invented provenance")
	}
	for _, bad := range [][]string{
		{"source", "save", "--id", "100"},
		{"source", "save", "--id", "100", "--input", "missing"},
		{"source", "list", "--id", "999"},
		{"source", "list", "--id", "100", "extra"},
	} {
		if out, code := runIn(t, vault, bad...); code != 1 || out["error"] == nil {
			t.Fatalf("accepted %v: %v", bad, out)
		}
	}
}

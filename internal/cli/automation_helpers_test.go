package cli

import "testing"

// titles pulls the "title" field out of a notes/results listing for easy assertions.
func titles(list []any) []string {
	out := make([]string, 0, len(list))
	for _, item := range list {
		out = append(out, item.(map[string]any)["title"].(string))
	}
	return out
}

func TestNotesUntaggedAndTagWritePath(t *testing.T) {
	vault := t.TempDir()
	if _, code := runIn(t, vault, "new", "--title", "Tagged", "--tag", "topic"); code != 0 {
		t.Fatalf("create tagged note failed")
	}
	if _, code := runIn(t, vault, "new", "--title", "Bare"); code != 0 {
		t.Fatalf("create bare note failed")
	}

	// The plain listing carries every real note (journals excluded), newest first.
	all, code := runIn(t, vault, "notes")
	if code != 0 {
		t.Fatalf("notes failed: %v", all)
	}
	if got := titles(all["notes"].([]any)); len(got) != 2 {
		t.Fatalf("expected both notes, got %v", got)
	}

	// --untagged pulls exactly the note that still needs tags.
	untagged, code := runIn(t, vault, "notes", "--untagged")
	if code != 0 {
		t.Fatalf("notes --untagged failed: %v", untagged)
	}
	if got := titles(untagged["notes"].([]any)); len(got) != 1 || got[0] != "Bare" {
		t.Fatalf("expected only Bare untagged, got %v", got)
	}

	// The write path: append merges a tag into the existing note, so it drops off the untagged list.
	if _, code := runIn(t, vault, "append", "--title", "Bare", "--tag", "topic"); code != 0 {
		t.Fatalf("append --tag failed")
	}
	after, code := runIn(t, vault, "notes", "--untagged")
	if code != 0 {
		t.Fatalf("notes --untagged (after) failed: %v", after)
	}
	if got := after["notes"].([]any); len(got) != 0 {
		t.Fatalf("expected no untagged notes after tagging, got %v", titles(got))
	}
}

func TestRefreshAllRunsPipeline(t *testing.T) {
	vault := t.TempDir()
	runIn(t, vault, "new", "--title", "One", "--id", "100")
	runIn(t, vault, "new", "--title", "Two", "--id", "200")

	run := func() map[string]any {
		out, code := runIn(t, vault, "refresh-all")
		if code != 0 {
			t.Fatalf("refresh-all failed: %v", out)
		}
		reindex := out["reindex"].(map[string]any)
		if reindex["indexed"].(float64) < 2 {
			t.Fatalf("expected at least 2 indexed notes, got %v", reindex["indexed"])
		}
		doc := out["doctor"].(map[string]any)
		if doc["ok"] != true {
			t.Fatalf("expected a clean doctor report, got %v", doc)
		}
		return out
	}

	// Idempotent: a second pass over the same vault yields the same clean result.
	run()
	run()
}

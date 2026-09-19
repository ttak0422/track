package cli

import "testing"

func TestCitePinnedEvidence(t *testing.T) {
	vault := t.TempDir()
	run := func(args ...string) map[string]any {
		t.Helper()
		out, code := runIn(t, vault, args...)
		if code != 0 {
			t.Fatalf("%v: %v", args, out)
		}
		return out
	}
	run("new", "--id", "100", "--title", "Original", "--body", "## Evidence\n\nOriginal claim ^proof\n\n## Other\nOther text\n")
	saved := run("source", "save", "--id", "100", "--source", "https://example.test", "--format", "text/html", "--at", "2026-09-19T00:00:00Z")
	version := saved["record"].(map[string]any)["version"].(string)
	run("rename", "--id", "100", "--to", "Renamed")
	run("update", "--id", "100", "--body", "## Evidence\nNew claim\n")
	run("reindex", "--full")
	old := run("cite", "--id", "100", "--version", version, "--heading", "Evidence")
	if old["title"] != "Renamed" || old["body"] != "## Evidence\n\nOriginal claim ^proof\n\n" || old["pinned"] != true || old["start_line"] != float64(1) || old["end_line"] != float64(4) {
		t.Fatalf("saved=%#v cite=%#v", saved, old)
	}
	if old["content_hash"] != saved["record"].(map[string]any)["content_hash"] {
		t.Fatal("citation did not return saved hash")
	}
	block := run("cite", "--id", "100", "--version", version, "--block", "proof")
	if block["body"] != "Original claim ^proof\n" {
		t.Fatal(block)
	}
	current := run("cite", "--id", "100", "--heading", "Evidence")
	if current["body"] != "## Evidence\nNew claim\n" || current["pinned"] != false || current["version"] != "" {
		t.Fatal(current)
	}
	for _, tail := range [][]string{
		{"--version", "missing"}, {"--version", version, "--heading", "Missing"},
		{"--version", version, "--start-line", "1", "--end-line", "99"},
		{"--version", version, "--page", "1"}, {"--page", "0"}, {"--version", ""},
		{"--heading", "Evidence", "--block", "proof"},
	} {
		if out, code := runIn(t, vault, append([]string{"cite", "--id", "100"}, tail...)...); code != 1 || out["error"] == nil {
			t.Fatalf("accepted %v: %v", tail, out)
		}
	}
	run("rm", "--id", "100")
	if out, code := runIn(t, vault, "cite", "--id", "100", "--version", version); code != 1 || out["error"] == nil {
		t.Fatalf("resolved deleted reference: %v", out)
	}
}

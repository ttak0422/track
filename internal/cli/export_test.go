package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportCommandRendersMarkdown(t *testing.T) {
	vault := t.TempDir()
	if _, code := runIn(t, vault, "new", "--title", "Doc", "--id", "100"); code != 0 {
		t.Fatalf("new failed")
	}
	path := filepath.Join(vault, "note", "100.md")
	body := "# Doc\n\nsee [[Doc]] and [今日](<journal?offset=0>)\n\n```lua :name hi :results output\nprint(1)\n```\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	out, code := capture(t, func() int { return Run([]string{"export", "--id", "100"}) })
	if code != 0 {
		t.Fatalf("export failed: %q", out)
	}
	if !strings.Contains(out, "see Doc and 今日") {
		t.Fatalf("wiki/action links not flattened: %q", out)
	}
	if strings.Contains(out, "[[") || strings.Contains(out, "<journal") {
		t.Fatalf("track syntax leaked into output: %q", out)
	}
	if !strings.Contains(out, "```lua\nprint(1)\n```") {
		t.Fatalf("babel header args not stripped: %q", out)
	}
}

func TestExportCommandWritesFile(t *testing.T) {
	vault := t.TempDir()
	if _, code := runIn(t, vault, "new", "--title", "Doc", "--id", "100"); code != 0 {
		t.Fatalf("new failed")
	}
	outPath := filepath.Join(vault, "out.md")

	raw, code := capture(t, func() int { return Run([]string{"export", "--id", "100", "--out", outPath}) })
	if code != 0 {
		t.Fatalf("export --out failed: %q", raw)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("--out should print JSON path, got %q", raw)
	}
	if decoded["path"] != outPath {
		t.Fatalf("unexpected path result: %v", decoded)
	}
	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "# Doc") {
		t.Fatalf("exported file missing body: %q", content)
	}
}

func TestExportCommandRequiresTarget(t *testing.T) {
	vault := t.TempDir()
	out, code := runIn(t, vault, "export")
	if code != 1 || !strings.Contains(out["error"].(string), "required") {
		t.Fatalf("expected target-required error, got code=%d out=%v", code, out)
	}
}

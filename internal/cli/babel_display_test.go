package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBabelRestoreUnsavedAndSuppressedDisplay(t *testing.T) {
	vault := t.TempDir()
	t.Setenv("TRACK_BABEL_SH", "sh")
	runIn(t, vault, "init")
	path := filepath.Join(vault, "note", "500.md")
	body := "```sh :name sample\nprintf hello\n```\n"
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	out, code := runIn(t, vault, "babel", "exec", "--path", path)
	if code != 0 || out["display"] != true {
		t.Fatalf("run = %v / %d", out, code)
	}
	out, code = runInWithStdin(t, vault, "\n"+body, "babel", "restore", "--path", path, "--body-stdin")
	if code != 0 {
		t.Fatal(out)
	}
	blocks := out["blocks"].([]any)
	if len(blocks) != 1 || blocks[0].(map[string]any)["end_line"] != float64(3) {
		t.Fatalf("restore = %v", out)
	}
	out, code = runInWithStdin(t, vault, "```sh :name sample\nprintf changed\n```\n", "babel", "restore", "--path", path, "--body-stdin")
	if code != 0 || len(out["blocks"].([]any)) != 0 {
		t.Fatalf("stale restore = %v", out)
	}
	for _, mode := range []string{"none", "discard", "silent"} {
		out, code = runInWithStdin(t, vault, "```sh :results "+mode+"\nprintf transient\n```", "babel", "exec", "--path", path, "--body-stdin")
		if code != 0 || out["stored"] != false || out["display"] != (mode == "silent") {
			t.Fatalf("%s = %v", mode, out)
		}
	}
}

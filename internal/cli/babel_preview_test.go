package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBabelPreviewDoesNotRunOrStore(t *testing.T) {
	vault := t.TempDir()
	runIn(t, vault, "init")
	path := filepath.Join(vault, "note", "505.md")
	body := "```sh :name part\ntouch should-not-exist\n```\n```sh :name main :noweb yes :eval no :var greeting=hi\n<<part>>\n```"
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	out, code := runIn(t, vault, "babel", "run", "--path", path, "--name", "main", "--var", "greeting=hello world", "--dry-run")
	if code != 0 || out["dry_run"] != true || out["body"] != "touch should-not-exist" || out["eval"] != "no" {
		t.Fatalf("preview = %v / %d", out, code)
	}
	if out["vars"].(map[string]any)["greeting"] != "hello world" {
		t.Fatalf("vars = %v", out)
	}
	for _, p := range []string{filepath.Join(vault, "note", "should-not-exist"), filepath.Join(vault, ".track", "notes", "505.yaml")} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("preview wrote %s: %v", p, err)
		}
	}
}

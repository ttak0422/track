package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBabelCacheIntegrity(t *testing.T) {
	vault := t.TempDir()
	t.Setenv("TRACK_BABEL_SH", "sh {{file}}")
	if _, code := runIn(t, vault, "new", "--title", "Cache", "--id", "520"); code != 0 {
		t.Fatal("new failed")
	}
	path := filepath.Join(vault, "note", "520.md")
	body := "```sh :name lib\necho one\n```\n```sh :name main :cache yes :noweb yes :var x=default\n<<lib>>\necho $x\necho ran >> runs\n```\n"
	write := func(s string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(s), 0600); err != nil {
			t.Fatal(err)
		}
	}
	run := func(cached bool, extra ...string) map[string]any {
		t.Helper()
		args := append([]string{"babel", "exec", "--id", "520", "--name", "main"}, extra...)
		out, code := runIn(t, vault, args...)
		if code != 0 || out["cached"] != cached {
			t.Fatalf("cached=%v: code=%d, %v", cached, code, out)
		}
		return out
	}
	write(body)
	run(false)
	run(true)
	if data, _ := os.ReadFile(filepath.Join(vault, "note", "runs")); string(data) != "ran\n" {
		t.Fatalf("cache ran process again: %q", data)
	}
	run(false, "--var", "x=override")
	run(true, "--var", "x=override")
	restored, code := runIn(t, vault, "babel", "restore", "--id", "520")
	if code != 0 || len(restored["blocks"].([]any)) != 0 {
		t.Fatalf("overridden inputs restored as defaults: %v", restored)
	}
	run(false)
	body = strings.ReplaceAll(body, "echo one", "echo two")
	write(body)
	restored, _ = runIn(t, vault, "babel", "restore", "--id", "520")
	if len(restored["blocks"].([]any)) != 0 {
		t.Fatalf("changed noweb restored: %v", restored)
	}
	if out := run(false); !strings.HasPrefix(out["stdout"].(string), "two\n") {
		t.Fatal(out)
	}
	t.Setenv("TRACK_BABEL_SH", "sh -e {{file}}")
	run(false)
	run(true)
	body = strings.ReplaceAll(body, ":cache yes", ":cache yes :eval query")
	write(body)
	run(false, "--yes")
	run(true, "--yes")
	if out, code := runIn(t, vault, "babel", "exec", "--id", "520", "--name", "main"); code != 1 || !strings.Contains(out["error"].(string), "--yes") {
		t.Fatalf("cache bypassed eval gate: %v", out)
	}
	body = strings.ReplaceAll(body, ":eval query", ":eval no")
	write(body)
	if _, code := runIn(t, vault, "babel", "exec", "--id", "520", "--name", "main", "--yes"); code != 1 {
		t.Fatal("eval no allowed")
	}
	write("```sh :name main :cache yes\necho fail >> failed-runs\nexit 1\n```\n")
	run(false)
	run(false)
	if data, _ := os.ReadFile(filepath.Join(vault, "note", "failed-runs")); string(data) != "fail\nfail\n" {
		t.Fatalf("failed run cached: %q", data)
	}
}

func TestBabelVariablesRejectStaleResultsAndHonorOverride(t *testing.T) {
	vault := t.TempDir()
	t.Setenv("TRACK_BABEL_SH", "sh {{file}}")
	runIn(t, vault, "new", "--title", "Vars", "--id", "521")
	path := filepath.Join(vault, "note", "521.md")
	body := "```sh :name base\necho before\n```\n```sh :name main :var x=base\necho $x\n```\n"
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	out, code := runIn(t, vault, "babel", "run", "--id", "521", "--name", "main", "--var", "x=override")
	if code != 0 || out["stdout"] != "override\n" {
		t.Fatalf("overridden missing reference resolved: %v", out)
	}
	runIn(t, vault, "babel", "exec", "--id", "521", "--name", "base")
	if err := os.WriteFile(path, []byte(strings.Replace(body, "echo before", "echo after", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	out, code = runIn(t, vault, "babel", "run", "--id", "521", "--name", "main")
	if code != 1 || !strings.Contains(out["error"].(string), "current successful inputs") {
		t.Fatalf("stale dependency accepted: %v", out)
	}
}

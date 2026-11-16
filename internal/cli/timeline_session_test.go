package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTimelineSessionWorkflow(t *testing.T) {
	vault := t.TempDir()
	runIn(t, vault, "init")
	call := func(args ...string) map[string]any {
		t.Helper()
		out, code := runIn(t, vault, append([]string{"timeline-session"}, args...)...)
		if code != 0 {
			t.Fatalf("%v: %v", args, out)
		}
		return out
	}
	call("init", "--project", "demo", "--title", "Demo")
	start := call("start", "--project", "demo", "--agent", "claude", "--session", "private-session")
	run := start["run"].(string)
	if strings.Contains(run, "private-session") {
		t.Fatal("raw session leaked")
	}
	base := []string{"--project", "demo", "--run", run}
	task := func(args ...string) map[string]any {
		return call(append(append([]string{"task"}, base...), args...)...)
	}
	task("--task", "work", "--title", "Implement", "--status", "active", "--remaining", "20")
	updated := task("--task", "work", "--done", "10")
	item := updated["tasks"].([]any)[0].(map[string]any)
	if item["title"] != "Implement" || item["remaining_minutes"] != float64(20) || item["status"] != "active" {
		t.Fatalf("patch lost fields: %v", item)
	}
	again := task("--task", "work", "--done", "10")
	if again["revision"] != updated["revision"] {
		t.Fatal("identical task appended")
	}
	zero := task("--task", "work", "--weight", "0")
	if zero["revision"] != updated["revision"] {
		t.Fatal("normalized weight duplicated")
	}
	resumed := call("start", "--project", "demo", "--agent", "claude", "--session", "private-session")
	if resumed["run"] != run || resumed["revision"] != updated["revision"] {
		t.Fatal("resume changed identity/journal")
	}
	reportArgs := append(append([]string{"report"}, base...), "--event", "milestone-1", "--summary", "Tests pass")
	report := call(reportArgs...)
	task("--task", "work", "--status", "done")
	retry := call(reportArgs...)
	if len(retry["reports"].([]any)) != 1 || report["reports"].([]any)[0].(map[string]any)["summary"] != "Tests pass" {
		t.Fatal("report retry duplicated")
	}
	bad := append([]string{}, reportArgs...)
	bad[len(bad)-1] = "different"
	if _, code := runIn(t, vault, append([]string{"timeline-session"}, bad...)...); code == 0 {
		t.Fatal("report identity reused")
	}
	task("--task", "child", "--title", "Child", "--parent", "work")
	child := task("--task", "child", "--parent", "")
	for _, item := range child["tasks"].([]any) {
		task := item.(map[string]any)
		if task["id"] == run+"-child" && task["parent_id"] != nil {
			t.Fatal("parent not cleared")
		}
	}
	other := call("start", "--project", "demo", "--agent", "codex", "--session", "private-session")
	if other["run"] == run {
		t.Fatal("agent identity collision")
	}
	raw, err := os.ReadFile(filepath.Join(vault, ".track", "timelines", "demo.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "private-session") {
		t.Fatal("raw session persisted")
	}
}

func TestTimelineSessionHooks(t *testing.T) {
	vault := t.TempDir()
	runIn(t, vault, "init")
	runIn(t, vault, "timeline-session", "init", "--project", "demo", "--title", "Demo")
	t.Setenv("TRACK_TIMELINE_ENABLED", "1")
	t.Setenv("TRACK_TIMELINE_PROJECT", "demo")
	for _, source := range []string{"startup", "resume", "compact", "clear"} {
		raw := `{"hook_event_name":"SessionStart","session_id":"same","source":"` + source + `","transcript_path":"/must/not/read","prompt":"SECRET"}`
		out, code := runInWithStdin(t, vault, raw, "timeline-session", "hook", "--agent", "claude")
		if code != 0 || out["hookSpecificOutput"] == nil {
			t.Fatalf("hook %s: %v", source, out)
		}
	}
	got, _ := runIn(t, vault, "timeline-project", "get", "--id", "demo")
	if got["revision"] != float64(2) || len(got["tasks"].([]any)) != 0 {
		t.Fatalf("lifecycle duplicated/completed work: %v", got)
	}
	raw, _ := os.ReadFile(filepath.Join(vault, ".track", "timelines", "demo.json"))
	if strings.Contains(string(raw), "SECRET") || strings.Contains(string(raw), "must/not/read") {
		t.Fatal("sensitive input persisted")
	}

	for _, session := range []string{"same", "forked"} {
		out, code := runInWithStdin(t, vault, `{"hook_event_name":"SessionStart","session_id":"`+session+`","source":"fork"}`, "timeline-session", "hook", "--agent", "codex")
		if code != 0 || out["hookSpecificOutput"] == nil {
			t.Fatal("codex fork failed")
		}
	}
	got, _ = runIn(t, vault, "timeline-project", "get", "--id", "demo")
	if got["revision"] != float64(4) {
		t.Fatal("new sessions did not separate")
	}
	t.Setenv("TRACK_TIMELINE_PROJECT", "missing")
	out, code := runSilentHook(t, vault, `{"hook_event_name":"SessionStart","session_id":"same"}`, "--agent", "claude")
	if code != 0 || out != "" {
		t.Fatal("missing project did not fail open")
	}
	t.Setenv("TRACK_TIMELINE_PROJECT", "demo")
	before, _ := os.ReadFile(filepath.Join(vault, ".track", "timelines", "demo.json"))
	// Hook failures must not block the normal session or echo untrusted content.
	for _, input := range []string{`{`, `{"hook_event_name":"SessionStart"}`, strings.Repeat("x", 65537), `{"hook_event_name":"Stop","session_id":"same"}`} {
		out, code := runSilentHook(t, vault, input, "--agent", "claude")
		if out != "" {
			t.Fatalf("unexpected hook stdout: %s", out)
		}
		if code != 0 {
			t.Fatal("hook blocked")
		}
	}
	t.Setenv("TRACK_TIMELINE_ENABLED", "0")
	if _, code := runSilentHook(t, vault, `not json`); code != 0 {
		t.Fatal("disabled hook read input")
	}
	after, _ := os.ReadFile(filepath.Join(vault, ".track", "timelines", "demo.json"))
	if string(before) != string(after) {
		t.Fatal("disabled/invalid hook modified store")
	}
}

func runSilentHook(t *testing.T, vault, input string, flags ...string) (string, int) {
	t.Helper()
	t.Setenv("TRACK_VAULT", vault)
	f, err := os.CreateTemp(t.TempDir(), "hook")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err = f.WriteString(input); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	os.Stdin = f
	defer func() { os.Stdin = old }()
	return capture(t, func() int { return Run(append([]string{"timeline-session", "hook"}, flags...)) })
}

func TestTimelineSessionRequiresBinding(t *testing.T) {
	t.Setenv("TRACK_VAULT", "")
	t.Setenv("TRACK_TIMELINE_PROJECT", "")
	_, code := capture(t, func() int { return Run([]string{"timeline-session", "init", "--title", "No"}) })
	if code == 0 {
		t.Fatal("default vault accepted")
	}
}

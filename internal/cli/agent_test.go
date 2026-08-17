package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runAgent runs Run with HOME isolated to a temp dir, so os.UserHomeDir points somewhere harmless.
func runAgent(t *testing.T, args ...string) (map[string]any, int) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	out, code := capture(t, func() int { return Run(args) })
	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("output is not JSON: %q (err %v)", out, err)
	}
	return decoded, code
}

// TestAgentLsWithoutClaude: with no ~/.claude, `track agent ls` is an empty list, exit 0, and it
// never touches the vault machinery (runAgent sets no TRACK_VAULT or config file at all).
func TestAgentLsWithoutClaude(t *testing.T) {
	out, code := runAgent(t, "agent", "ls")
	if code != 0 {
		t.Fatalf("agent ls exit = %d, want 0", code)
	}
	if _, ok := out["sessions"]; !ok {
		t.Fatalf("expected a sessions key, got %v", out)
	}
	if got := out["sessions"].([]any); len(got) != 0 {
		t.Fatalf("expected no sessions, got %v", got)
	}
}

// TestAgentLsRejectsArgs: `track agent ls` takes no arguments.
func TestAgentLsRejectsArgs(t *testing.T) {
	out, code := runAgent(t, "agent", "ls", "stray")
	if code != 1 {
		t.Fatalf("agent ls stray exit = %d, want 1", code)
	}
	if _, ok := out["error"]; !ok {
		t.Fatalf("expected an error, got %v", out)
	}
}

// TestAgentLogMissingSession: an unknown session id is a JSON error, exit 1.
func TestAgentLogMissingSession(t *testing.T) {
	out, code := runAgent(t, "agent", "log", "no-such-session")
	if code != 1 {
		t.Fatalf("agent log exit = %d, want 1", code)
	}
	if !strings.Contains(out["error"].(string), "no transcript") {
		t.Fatalf("unexpected error: %v", out["error"])
	}
}

// TestAgentLogBadTail: a non-positive --tail is refused.
func TestAgentLogBadTail(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".claude", "projects", "x")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sess.jsonl"), []byte("{\"type\":\"user\",\"message\":{\"content\":[]}}\n"), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	out, code := runAgent(t, "agent", "log", "sess", "--tail", "0")
	if code != 1 {
		t.Fatalf("agent log --tail 0 exit = %d, want 1", code)
	}
	if _, ok := out["error"]; !ok {
		t.Fatalf("expected an error, got %v", out)
	}
}

// TestAgentMissingSubcommand: a bare `track agent` is a usage error.
func TestAgentMissingSubcommand(t *testing.T) {
	out, code := runAgent(t, "agent")
	if code != 1 {
		t.Fatalf("agent exit = %d, want 1", code)
	}
	if _, ok := out["error"]; !ok {
		t.Fatalf("expected an error, got %v", out)
	}
}

// TestAgentLogHappyPath: with a real transcript under $HOME, `agent log` emits the Transcript
// object directly — not wrapped in a {"log":...} envelope — with messages, aiTitle and pr.
func TestAgentLogHappyPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".claude", "projects", "-w")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	lines := []string{
		`{"type":"user","uuid":"u1","timestamp":"t1","message":{"content":[{"type":"text","text":"hi"}]}}`,
		`{"type":"assistant","uuid":"a1","timestamp":"t2","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"ls"}}]}}`,
		`{"type":"ai-title","aiTitle":"タイトル"}`,
		`{"type":"pr-link","prNumber":7,"prUrl":"https://github.com/x/y/pull/7","prRepository":"x/y"}`,
		`{"type":"user","uuid":"u2","timestamp":"t3","message":{"role":"user","content":"done"}}`,
	}
	if err := os.WriteFile(filepath.Join(dir, "sess.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	// Flags after the positional id, as the CLI contract documents. --tail 5 exceeds the two
	// messages in the fixture, so the scan reaches the file start and picks up ai-title and pr.
	out, code := capture(t, func() int { return Run([]string{"agent", "log", "sess", "--tail", "5"}) })
	if code != 0 {
		t.Fatalf("agent log exit = %d, out=%s", code, out)
	}
	var tr map[string]any
	if err := json.Unmarshal([]byte(out), &tr); err != nil {
		t.Fatalf("output is not JSON: %q (err %v)", out, err)
	}
	if _, wrapped := tr["log"]; wrapped {
		t.Fatalf("transcript must be emitted directly, not wrapped: %s", out)
	}
	if tr["sessionId"] != "sess" || tr["aiTitle"] != "タイトル" {
		t.Fatalf("sessionId/aiTitle wrong: %v", tr)
	}
	pr := tr["pr"].(map[string]any)
	if pr["number"].(float64) != 7 || pr["url"] != "https://github.com/x/y/pull/7" || pr["repository"] != "x/y" {
		t.Fatalf("pr wrong: %v", pr)
	}
	msgs := tr["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	// Chronological order: the older user message first, the newest one last.
	if msgs[0].(map[string]any)["uuid"] != "u1" {
		t.Fatalf("first message wrong: %v", msgs[0])
	}
	last := msgs[2].(map[string]any)
	if last["type"] != "user" || last["uuid"] != "u2" {
		t.Fatalf("last message wrong: %v", last)
	}
	blocks := last["message"].([]any)
	first := blocks[0].(map[string]any)
	if first["type"] != "text" || first["text"] != "done" {
		t.Fatalf("string content should become a text block: %v", blocks)
	}
}

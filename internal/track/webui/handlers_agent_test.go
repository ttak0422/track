package webui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/store"
)

// agentHomeDir creates a fixture home for the live-session endpoints, with ~/.claude/sessions ready.
func agentHomeDir(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude", "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	return home
}

// writeAgentSession writes one live session record. No procStart is written, so the liveness check
// reduces to the process-exists probe — a live child process is enough to keep the record.
func writeAgentSession(t *testing.T, home string, pid int, sessionID, cwd string) {
	t.Helper()
	rec := `{"pid":` + strconv.Itoa(pid) + `,"sessionId":"` + sessionID + `","cwd":"` + cwd + `","startedAt":1,"kind":"interactive","name":"n","updatedAt":2,"status":"idle","statusUpdatedAt":3}`
	if err := os.WriteFile(filepath.Join(home, ".claude", "sessions", strconv.Itoa(pid)+".json"), []byte(rec), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeAgentTranscript writes a session's transcript under projects/<slug>/<session>.jsonl.
func writeAgentTranscript(t *testing.T, home, sessionID string, lines []string) {
	t.Helper()
	projDir := filepath.Join(home, ".claude", "projects", "-w-slug")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(projDir, sessionID+".jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// livePID spawns a short-lived child whose pid is a live process for the liveness probe.
func livePID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
	})
	return cmd.Process.Pid
}

// gitRepo creates a git work tree with the given initial branch and returns its root. A branch
// without any commit is enough: branch --show-current reads HEAD's symbolic ref.
func gitRepo(t *testing.T, name, branch string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "init", "-b", branch)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init -b %s: %v (%s)", branch, err, out)
	}
	return root
}

// TestAgentsEndpoint covers the note/branch decoration of /api/agents: a session whose repo has a
// vault note gets note + branch, a repo the vault has no note for gets branch alone, and a cwd that
// is not a git work tree gets neither — all without error.
func TestAgentsEndpoint(t *testing.T) {
	cfg := &config.Config{
		VaultDir:          t.TempDir(),
		DBPath:            filepath.Join(t.TempDir(), "index.db"),
		Extensions:        []string{".md"},
		DateFormat:        "2006-01-02",
		JournalDateFormat: "20060102",
	}
	s, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	// The vault's only note is titled after the tracked repo, the same title a session's cwd resolves to.
	addIndexedTestNote(t, cfg, s, 100, "track")

	repo := gitRepo(t, "track", "feat/status")
	untracked := gitRepo(t, "untracked", "main")
	scratch := t.TempDir() // a cwd outside git

	home := agentHomeDir(t)
	writeAgentSession(t, home, livePID(t), "s-track", repo)
	writeAgentSession(t, home, livePID(t), "s-untracked", untracked)
	writeAgentSession(t, home, livePID(t), "s-scratch", scratch)
	t.Setenv("HOME", home)

	server := httptest.NewServer(New(cfg, s).Handler())
	t.Cleanup(server.Close)

	agents := getJSON(t, server.URL+"/api/agents")
	raw := agents["sessions"].([]any)
	if len(raw) != 3 {
		t.Fatalf("sessions = %d, want 3: %v", len(raw), raw)
	}
	bySession := map[string]map[string]any{}
	for _, item := range raw {
		m := item.(map[string]any)
		bySession[m["sessionId"].(string)] = m
	}

	tracked := bySession["s-track"]
	if tracked["branch"] != "feat/status" {
		t.Fatalf("tracked branch = %v, want feat/status: %v", tracked["branch"], tracked)
	}
	note, ok := tracked["note"].(map[string]any)
	if !ok {
		t.Fatalf("tracked session should resolve a note: %v", tracked)
	}
	if note["note_id"].(float64) != 100 || note["title"] != "track" {
		t.Fatalf("note = %v, want {note_id:100 title:track}", note)
	}

	un := bySession["s-untracked"]
	if _, ok := un["note"]; ok {
		t.Fatalf("repo without a vault note must omit it: %v", un)
	}
	if un["branch"] != "main" {
		t.Fatalf("untracked branch = %v, want main", un["branch"])
	}

	sc := bySession["s-scratch"]
	if _, ok := sc["note"]; ok {
		t.Fatalf("non-git cwd must omit note: %v", sc)
	}
	if _, ok := sc["branch"]; ok {
		t.Fatalf("non-git cwd must omit branch: %v", sc)
	}
}

// TestAgentsEndpointNilStore: a server built with New(cfg, nil) still answers /api/agents — sessions
// come back with no note decoration, because there is no vault to resolve against.
func TestAgentsEndpointNilStore(t *testing.T) {
	home := agentHomeDir(t)
	writeAgentSession(t, home, livePID(t), "s-x", t.TempDir())
	t.Setenv("HOME", home)

	server := httptest.NewServer(New(&config.Config{}, nil).Handler())
	t.Cleanup(server.Close)

	agents := getJSON(t, server.URL+"/api/agents")
	raw := agents["sessions"].([]any)
	if len(raw) != 1 {
		t.Fatalf("sessions = %d, want 1: %v", len(raw), raw)
	}
	if _, ok := raw[0].(map[string]any)["note"]; ok {
		t.Fatalf("nil store must not decorate sessions with a note: %v", raw[0])
	}
}

// TestAgentLogEndpoint covers /api/agent/log: a transcript comes back as the Transcript shape (not
// wrapped), tail defaults to 50, and the error paths are a 400 for a missing/illegal id or tail and
// a 404 for a session with no transcript.
func TestAgentLogEndpoint(t *testing.T) {
	home := agentHomeDir(t)
	writeAgentTranscript(t, home, "s-log", []string{
		`{"type":"user","uuid":"u1","timestamp":"t1","message":{"content":[{"type":"text","text":"first"}]}}`,
		`{"type":"assistant","uuid":"a1","timestamp":"t2","message":{"content":[{"type":"text","text":"hello"}]}}`,
		`{"type":"ai-title","aiTitle":"進捗"}`,
	})
	t.Setenv("HOME", home)

	server := httptest.NewServer(New(&config.Config{}, nil).Handler())
	t.Cleanup(server.Close)

	tr := getJSON(t, server.URL+"/api/agent/log?id=s-log&tail=10")
	if tr["sessionId"] != "s-log" || tr["aiTitle"] != "進捗" {
		t.Fatalf("transcript = %v, want sessionId s-log and aiTitle 進捗", tr)
	}
	if msgs, _ := tr["messages"].([]any); len(msgs) != 2 {
		t.Fatalf("messages = %d, want 2: %v", len(msgs), tr["messages"])
	}

	// tail omitted defaults to 50, still the full two-message tail here.
	def := getJSON(t, server.URL+"/api/agent/log?id=s-log")
	if msgs, _ := def["messages"].([]any); len(msgs) != 2 {
		t.Fatalf("default-tail messages = %d, want 2: %v", len(msgs), def["messages"])
	}

	status := func(url string) int {
		t.Helper()
		resp, err := http.Get(url)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	if got := status(server.URL + "/api/agent/log?id=nope"); got != http.StatusNotFound {
		t.Fatalf("unknown session status = %d, want 404", got)
	}
	if got := status(server.URL + "/api/agent/log"); got != http.StatusBadRequest {
		t.Fatalf("missing id status = %d, want 400", got)
	}
	if got := status(server.URL + "/api/agent/log?id=s-log&tail=0"); got != http.StatusBadRequest {
		t.Fatalf("tail=0 status = %d, want 400", got)
	}
	if got := status(server.URL + "/api/agent/log?id=s-log&tail=-3"); got != http.StatusBadRequest {
		t.Fatalf("tail=-3 status = %d, want 400", got)
	}
	if got := status(server.URL + "/api/agent/log?id=s-log&tail=abc"); got != http.StatusBadRequest {
		t.Fatalf("tail=abc status = %d, want 400", got)
	}
}
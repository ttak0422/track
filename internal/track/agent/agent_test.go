package agent

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// writeSession writes a session record into a temp ~/.claude/sessions tree.
func writeSession(t *testing.T, home, name, body string) string {
	t.Helper()
	dir := filepath.Join(home, ".claude", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write session: %v", err)
	}
	return path
}

// fixedLstart is a plausible UTC start time to pair with fake probe results.
const fixedLstart = "Mon Aug 17 10:39:41 2026"

func TestList(t *testing.T) {
	livePID := os.Getpid() // kill(pid,0) succeeds for ourselves; ps is faked per pid
	deadPID := 99999999    // no such process: kill reports ESRCH

	home := t.TempDir()
	writeSession(t, home, "1.json", `{"pid":`+strconv.Itoa(livePID)+`,"sessionId":"s-live","cwd":"/w","startedAt":1,"procStart":"`+fixedLstart+`","version":"2.1.233","kind":"interactive","name":"live","updatedAt":2,"status":"idle","statusUpdatedAt":3}`)
	writeSession(t, home, "2.json", `{"pid":`+strconv.Itoa(deadPID)+`,"sessionId":"s-dead","cwd":"/w","startedAt":1,"procStart":"`+fixedLstart+`","kind":"interactive","name":"dead","updatedAt":2,"status":"idle","statusUpdatedAt":3}`)
	writeSession(t, home, "3.json", `{"pid":`+strconv.Itoa(livePID)+`,"sessionId":"s-waiting","cwd":"/w","startedAt":1,"procStart":"`+fixedLstart+`","kind":"bg","name":"bg","updatedAt":2,"status":"waiting","statusUpdatedAt":3,"waitingFor":"permission prompt"}`)
	writeSession(t, home, "4.json", `{"pid":`+strconv.Itoa(livePID)+`,"sessionId":"s-mismatch","cwd":"/w","startedAt":1,"procStart":"Mon Aug 10 00:00:00 2026","kind":"interactive","name":"mismatch","updatedAt":2,"status":"idle","statusUpdatedAt":3}`)
	writeSession(t, home, "5.json", `{"pid":`+strconv.Itoa(livePID)+`,"sessionId":"s-corrupt","cwd":"/w","startedAt":1,"kind":"interactive","name":"corrupt","updatedAt":2,"status":"idle","statusUpdatedAt":3`) // truncated JSON
	writeSession(t, home, "6.key", `{"pid":`+strconv.Itoa(livePID)+`,"sessionId":"s-key","kind":"interactive","name":"key","updatedAt":2,"status":"idle","statusUpdatedAt":3}`)

	probe := processProbe{ps: func(pid int) (string, string, error) {
		switch pid {
		case livePID:
			return "claude", fixedLstart, nil
		default:
			return "", "", os.ErrProcessDone
		}
	}}
	got, err := listSessions(home, probe)
	if err != nil {
		t.Fatalf("listSessions: %v", err)
	}

	// pid 1001: kill(1001,0) is ESRCH → dropped before ps is consulted.
	// deadPID: ESRCH → dropped.
	// livePID×3: kill passes, ps returns a session cmdline with a matching lstart.
	//   - s-live:    kept (a plain interactive session).
	//   - s-waiting: kept, with waitingFor and kind=bg.
	//   - s-mismatch: dropped — procStart differs from the ps lstart.
	//   - s-corrupt:  file is not JSON → skipped.
	// 6.key: not a .json file → skipped.
	var ids []string
	for _, s := range got {
		ids = append(ids, s.SessionID)
	}
	if want := []string{"s-live", "s-waiting"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("live sessions = %v, want %v", ids, want)
	}
	for _, s := range got {
		if s.SessionID == "s-waiting" {
			if s.WaitingFor != "permission prompt" {
				t.Fatalf("s-waiting waitingFor = %q, want %q", s.WaitingFor, "permission prompt")
			}
			if s.Kind != "bg" {
				t.Fatalf("s-waiting kind = %q, want bg", s.Kind)
			}
		} else if s.WaitingFor != "" {
			t.Fatalf("session %s should not carry waitingFor, got %q", s.SessionID, s.WaitingFor)
		}
	}
}

// TestListNoClaudeDir: a machine without ~/.claude (or without the sessions dir) is an empty list.
func TestListNoClaudeDir(t *testing.T) {
	got, err := List(t.TempDir()) // real probe, but nothing to probe
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no sessions, got %v", got)
	}
}

// TestListSortsByPID: sessions come out pid-sorted regardless of directory order. Distinct live
// pids come from short-lived child processes, so kill(pid,0) passes and only ps is faked.
func TestListSortsByPID(t *testing.T) {
	cmds := []*exec.Cmd{}
	for i := 0; i < 3; i++ {
		cmd := exec.Command("sleep", "60")
		if err := cmd.Start(); err != nil {
			t.Fatalf("spawn child: %v", err)
		}
		cmds = append(cmds, cmd)
		t.Cleanup(func() {
			cmd.Process.Kill()
			cmd.Wait()
		})
	}
	pids := []int{cmds[2].Process.Pid, cmds[0].Process.Pid, cmds[1].Process.Pid} // deliberately out of order
	home := t.TempDir()
	for i, pid := range pids {
		writeSession(t, home, strconv.Itoa(pid)+".json",
			`{"pid":`+strconv.Itoa(pid)+`,"sessionId":"s-`+strconv.Itoa(i)+`","cwd":"/w","startedAt":1,"procStart":"`+fixedLstart+`","kind":"interactive","name":"n","updatedAt":2,"status":"idle","statusUpdatedAt":3}`)
	}
	probe := processProbe{ps: func(int) (string, string, error) { return "claude", fixedLstart, nil }}
	got, err := listSessions(home, probe)
	if err != nil {
		t.Fatalf("listSessions: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 sessions, got %d (%+v)", len(got), got)
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].PID > got[i].PID {
			t.Fatalf("sessions not pid-sorted: %+v", got)
		}
	}
}

// TestListIgnoresUnknownFields: a forward-compatible record (new fields) still decodes.
func TestListIgnoresUnknownFields(t *testing.T) {
	livePID := os.Getpid()
	home := t.TempDir()
	writeSession(t, home, "x.json",
		`{"pid":`+strconv.Itoa(livePID)+`,"sessionId":"s-x","cwd":"/w","startedAt":1,"procStart":"`+fixedLstart+`","kind":"interactive","name":"n","updatedAt":2,"status":"busy","statusUpdatedAt":3,"tempo":"active","state":"working","detail":"","needs":"tool_use","tmux":true,"jobId":"j","futureField":{"a":1}}`)
	probe := processProbe{ps: func(int) (string, string, error) { return "claude", fixedLstart, nil }}
	got, err := listSessions(home, probe)
	if err != nil {
		t.Fatalf("listSessions: %v", err)
	}
	if len(got) != 1 || got[0].SessionID != "s-x" || got[0].Status != "busy" {
		t.Fatalf("expected the future-field session to decode, got %+v", got)
	}
}

func TestIsNonSession(t *testing.T) {
	cases := []struct {
		cmdline string
		want    bool
	}{
		{"claude", false}, // bare `claude`: an interactive session
		{"/Users/x/.local/bin/claude", false},
		// A session record naming a bg-spare IS the background session (kind=bg); it must stay.
		// The full argument line ends in .claim.sock for claimed and unclaimed spares alike, so
		// nothing here may be filtered on.
		{"claude bg-spare --bg-spare /tmp/cc-daemon-501/aa080385/spare/d37d37a1.claim.sock", false},
		{"claude bg-spare --bg-spare /tmp/cc-daemon-501/aa080385/spare/d61b365f.claim.sock", false},
		// macOS truncates the command column to 16 chars when combined with another ps column:
		// only the first program token and first argument survive, and `daemon` is the one
		// infrastructure word that fits.
		{"claude bg-spare", false},  // what ps -o command=,lstart= shows for a bg session
		{"claude bg-pty-ho", false}, // what ps -o command=,lstart= shows for a PTY host
		{"claude bg-pty-host --bg-pty-host /tmp/x.pty.sock 200 50 -- /usr/bin/claude --bg-spare /tmp/y.claim.sock", false}, // PTY hosts are not sessions but never get a session record; keep it simple
		{"claude daemon run --json-path /Users/tak/.claude", true},                                                         // the daemon: never a session
		{"claude daemon", true}, // the truncated form that survives macOS's 16-char cut
		{"nginx", false},        // not claude infrastructure; procStart guards pid reuse instead
	}
	for _, tc := range cases {
		if got := isNonSession(tc.cmdline); got != tc.want {
			t.Errorf("isNonSession(%q) = %v, want %v", tc.cmdline, got, tc.want)
		}
	}
}

func TestSameStart(t *testing.T) {
	cases := []struct {
		procStart, lstart string
		want              bool
	}{
		{"Mon Aug 17 10:39:41 2026", "Mon Aug 17 10:39:41 2026", true},  // identical
		{"Mon Aug 17 10:39:41 2026", "Mon Aug  5 14:04:30 2026", false}, // different day
		{"Mon Aug 17 10:39:41 2026", "not a time", false},               // unparseable
		{"not a time", "Mon Aug 17 10:39:41 2026", false},
	}
	for _, tc := range cases {
		if got := sameStart(tc.procStart, tc.lstart); got != tc.want {
			t.Errorf("sameStart(%q, %q) = %v, want %v", tc.procStart, tc.lstart, got, tc.want)
		}
	}
}

// transcriptFixture writes a projects/<slug>/<session>.jsonl transcript.
func transcriptFixture(t *testing.T, home, sessionID string, lines []string) string {
	t.Helper()
	dir := filepath.Join(home, ".claude", "projects", "-w-slug")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir projects: %v", err)
	}
	path := filepath.Join(dir, sessionID+".jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	return path
}

func TestLog(t *testing.T) {
	home := t.TempDir()
	const sid = "2e63420b-e839-4903-9f56-2262b235b5e5"
	transcriptFixture(t, home, sid, []string{
		`{"type":"system","subtype":"init"}`,
		`{"type":"unknown-future-type","something":1}`,
		`{"type":"user","uuid":"u1","timestamp":"t1","cwd":"/w","gitBranch":"main","message":{"role":"user","content":[{"type":"text","text":"first"}]}}`,
		`{"type":"assistant","uuid":"a1","timestamp":"t2","message":{"content":[{"type":"text","text":"hello"},{"type":"tool_use","id":"tu1","name":"Bash","input":{"command":"ls"}}]}}`,
		`{"type":"ai-title","aiTitle":"早いタイトル"}`,
		`{"type":"user","uuid":"u2","timestamp":"t3","message":{"role":"user","content":"plain string content"}}`,
		`{"type":"attachment","content":"ignore me"}`,
		`{"type":"assistant","uuid":"a2","timestamp":"t4","message":{"content":[{"type":"tool_use","name":"Edit","input":{"file":"x.go"}},{"type":"text","text":"done"}]}}`,
		`{"type":"pr-link","prNumber":169,"prUrl":"https://github.com/ttak0422/track/pull/169","prRepository":"ttak0422/track"}`,
		`{"type":"ai-title","aiTitle":"最新タイトル"}`,
		`{"type":"user","uuid":"u3","timestamp":"t5","cwd":"/w","gitBranch":"feat/x","message":{"role":"user","content":[{"type":"text","text":"final"},{"type":"image","source":{"type":"base64","data":"AAAA"}}]}}`,
		`this line is not json`,
	})

	tr, err := Log(home, sid, 2)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if tr.SessionID != sid {
		t.Fatalf("sessionId = %q, want %q", tr.SessionID, sid)
	}
	// Latest ai-title wins; the pr-link was also picked up.
	if tr.AITitle != "最新タイトル" {
		t.Fatalf("aiTitle = %q, want 最新タイトル", tr.AITitle)
	}
	if tr.PR == nil || tr.PR.Number != 169 || tr.PR.URL != "https://github.com/ttak0422/track/pull/169" || tr.PR.Repository != "ttak0422/track" {
		t.Fatalf("pr = %+v, want PR 169", tr.PR)
	}
	// tail=2 keeps the last two user/assistant messages, in chronological order.
	if len(tr.Messages) != 2 {
		t.Fatalf("len(messages) = %d, want 2: %+v", len(tr.Messages), tr.Messages)
	}
	if tr.Messages[0].UUID != "a2" || tr.Messages[1].UUID != "u3" {
		t.Fatalf("messages = %+v, want [a2, u3]", tr.Messages)
	}
	// The assistant message's tool_use keeps name and input; text is next to it.
	if len(tr.Messages[0].Message) != 2 || tr.Messages[0].Message[0].Type != "tool_use" ||
		tr.Messages[0].Message[0].Name != "Edit" || tr.Messages[0].Message[0].Input["file"] != "x.go" {
		t.Fatalf("tool_use block not preserved: %+v", tr.Messages[0].Message)
	}
	// The user message's string content becomes a text block; the image block keeps only its type.
	last := tr.Messages[1].Message
	if len(last) != 2 || last[0].Type != "text" || last[0].Text != "final" || last[1].Type != "image" || last[1].Text != "" {
		t.Fatalf("user content blocks wrong: %+v", last)
	}
	if tr.Messages[1].GitBranch != "feat/x" {
		t.Fatalf("gitBranch = %q, want feat/x", tr.Messages[1].GitBranch)
	}
}

func TestLogTailLargerThanTranscript(t *testing.T) {
	home := t.TempDir()
	const sid = "sess-big-tail"
	transcriptFixture(t, home, sid, []string{
		`{"type":"user","uuid":"u1","timestamp":"t1","message":{"content":[{"type":"text","text":"only"}]}}`,
	})
	tr, err := Log(home, sid, 50)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if len(tr.Messages) != 1 || tr.Messages[0].UUID != "u1" {
		t.Fatalf("expected the single message, got %+v", tr.Messages)
	}
}

func TestLogNoTitleInTailWindow(t *testing.T) {
	home := t.TempDir()
	const sid = "sess-no-title"
	// ai-title sits far before the messages; the tail scan for 1 message never reaches it.
	transcriptFixture(t, home, sid, []string{
		`{"type":"ai-title","aiTitle":"遠いタイトル"}`,
		`{"type":"user","uuid":"u1","timestamp":"t1","message":{"content":[{"type":"text","text":"x"}]}}`,
	})
	tr, err := Log(home, sid, 1)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if tr.AITitle != "" {
		t.Fatalf("aiTitle = %q, want empty (outside the tail window)", tr.AITitle)
	}
	if len(tr.Messages) != 1 {
		t.Fatalf("expected 1 message, got %+v", tr.Messages)
	}
}

func TestLogErrors(t *testing.T) {
	home := t.TempDir()
	if _, err := Log(home, "no-such-session", 10); err == nil {
		t.Fatal("expected error for missing transcript")
	}
	transcriptFixture(t, home, "sess-x", []string{`{"type":"user","message":{"content":[]}}`})
	if _, err := Log(home, "sess-x", 0); err == nil {
		t.Fatal("expected error for non-positive tail")
	}
	if _, err := Log(home, "sess-x", -3); err == nil {
		t.Fatal("expected error for negative tail")
	}
}

func TestScanTranscriptTailStopsAtNeed(t *testing.T) {
	home := t.TempDir()
	const sid = "sess-many"
	lines := make([]string, 0, 200)
	for i := 0; i < 200; i++ {
		lines = append(lines, `{"type":"user","uuid":"u`+strconv.Itoa(i)+`","timestamp":"t`+strconv.Itoa(i)+`","message":{"content":[{"type":"text","text":"m"}]}}`)
	}
	transcriptFixture(t, home, sid, lines)
	tr, err := Log(home, sid, 3)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if len(tr.Messages) != 3 {
		t.Fatalf("len = %d, want 3", len(tr.Messages))
	}
	// The last three lines, chronological order.
	if tr.Messages[0].UUID != "u197" || tr.Messages[1].UUID != "u198" || tr.Messages[2].UUID != "u199" {
		t.Fatalf("messages = %+v, want [u197, u198, u199]", tr.Messages)
	}
}

// TestListJSONContract locks the exact JSON keys of the wire contract.
func TestListJSONContract(t *testing.T) {
	s := Session{
		PID: 11499, SessionID: "s1", CWD: "/w", StartedAt: 1, ProcStart: fixedLstart,
		Version: "2.1.233", Kind: "interactive", Name: "n", UpdatedAt: 2,
		Status: "waiting", StatusUpdatedAt: 3, WaitingFor: "permission prompt",
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"pid", "sessionId", "cwd", "startedAt", "procStart", "version", "kind", "name", "updatedAt", "status", "statusUpdatedAt", "waitingFor"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing contract key %q in %s", key, raw)
		}
	}
	s2 := s
	s2.WaitingFor = ""
	raw2, _ := json.Marshal(s2)
	if strings.Contains(string(raw2), "waitingFor") {
		t.Errorf("waitingFor should be omitted when empty: %s", raw2)
	}
}

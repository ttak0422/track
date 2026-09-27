package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/request"
	"github.com/ttak0422/track/internal/track/store"
)

// createUpdateHTTP creates and claims an update request against note 100 in the test vault,
// returning the request id and its current dispatch id. The target ETag is read fresh from the
// note, exactly as the browser would have done when it sent the request.
func createUpdateHTTP(t *testing.T, server *httptest.Server, cfg *config.Config, instruction string) (string, string) {
	t.Helper()
	code, created := postRequest(t, server.URL+"/api/requests",
		`{"intent":"update","instruction":"`+instruction+`","agent_id":"a","update_target":{"note_id":100,"etag":"`+testNoteETag(t, cfg)+`"}}`)
	if code != http.StatusAccepted {
		t.Fatalf("update create = %d: %v", code, created)
	}
	id := reqID(t, created)
	dispatch := currentDispatchID(t, created)
	if code, _ := postRequest(t, server.URL+"/api/requests/"+id+"/claim", `{"dispatch_id":"`+dispatch+`"}`); code != http.StatusOK {
		t.Fatalf("claim = %d", code)
	}
	return id, dispatch
}

// submitUpdateResult posts an update result, which the server applies synchronously, and returns
// the settled request detail.
func submitUpdateResult(t *testing.T, server *httptest.Server, id, dispatch, proposed, answer string) map[string]any {
	t.Helper()
	code, detail := postRequest(t, server.URL+"/api/requests/"+id+"/result",
		`{"dispatch_id":"`+dispatch+`","proposed_body":"`+proposed+`","answer_markdown":"`+answer+`"}`)
	if code != http.StatusOK {
		t.Fatalf("update result = %d: %v", code, detail)
	}
	return detail
}

func noteBody(t *testing.T, cfg *config.Config, id int64) string {
	t.Helper()
	raw, err := os.ReadFile(cfg.NotePath(id))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// tempLeftovers reports whether any hidden or .tmp file lingers under dir — the failure mode an
// atomic write's temp+rename discipline is supposed to prevent.
func tempLeftovers(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") || strings.HasSuffix(e.Name(), ".tmp") {
			out = append(out, e.Name())
		}
	}
	return out
}

func TestUpdateApplyAppliesNoteAtomically(t *testing.T) {
	server, cfg := requestServer(t)
	id, dispatch := createUpdateHTTP(t, server, cfg, "Make it clearer.")

	detail := submitUpdateResult(t, server, id, dispatch, "# Rewritten\\n", "Clarified.")
	if detail["status"] != "completed" {
		t.Fatalf("applied update must be completed, got %v", detail["status"])
	}
	// The note file holds exactly the proposal (with the engine's trailing-newline rule).
	if got := noteBody(t, cfg, 100); got != "# Rewritten\n" {
		t.Fatalf("note body after apply = %q, want the proposal", got)
	}
	// The before/after body+ETag and the change rationale are recorded on the result.
	res := detail["result"].(map[string]any)
	ap := res["apply"].(map[string]any)
	if ap["before_body"] != "# Alpha\n" || ap["after_body"] != "# Rewritten\n" || ap["reason"] != "Clarified." || ap["applied_at"] == "" {
		t.Fatalf("apply record wrong: %v", ap)
	}
	if ap["before_etag"] == ap["after_etag"] {
		t.Fatalf("before and after ETags must differ after a real change")
	}
	// The proposal and the fingerprint stay on the result.
	if res["proposed_body"] != "# Rewritten\n" || res["fingerprint"] == "" {
		t.Fatalf("proposal not preserved: %v", res)
	}
	// No temp files leaked into the note directory or the requests directory.
	if leftovers := tempLeftovers(t, cfg.NoteDir()); len(leftovers) != 0 {
		t.Fatalf("note dir left temp files after apply: %v", leftovers)
	}
	if leftovers := tempLeftovers(t, filepath.Join(cfg.TrackDir(), "requests")); len(leftovers) != 0 {
		t.Fatalf("requests dir left temp files after apply: %v", leftovers)
	}
	// The apply is durable: the stored request file carries it.
	raw, err := os.ReadFile(filepath.Join(cfg.TrackDir(), "requests", id+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"apply"`) {
		t.Fatalf("stored request must carry the apply record: %s", raw)
	}
}

// TestUpdateApplyETagRaceOneWinner drives two update requests that captured the same send-time ETag
// through the result step concurrently. Exactly one may replace the note; the other must settle to
// conflict with its proposal and the reason preserved — never an overwrite.
func TestUpdateApplyETagRaceOneWinner(t *testing.T) {
	server, cfg := requestServer(t)
	id1, d1 := createUpdateHTTP(t, server, cfg, "First proposal.")
	id2, d2 := createUpdateHTTP(t, server, cfg, "Second proposal.")
	if testNoteETag(t, cfg) != testNoteETag(t, cfg) {
		t.Fatal("both updates must share the same send-time ETag")
	}

	var wg sync.WaitGroup
	results := make([]struct {
		code int
		resp map[string]any
		err  error
	}, 2)
	post := func(i int, id, dispatch string) {
		defer wg.Done()
		body := `{"dispatch_id":"` + dispatch + `","proposed_body":"# Proposal ` + string(rune('1'+i)) + `\n"}`
		req, err := http.NewRequest(http.MethodPost, server.URL+"/api/requests/"+id+"/result", strings.NewReader(body))
		if err != nil {
			results[i].err = err
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer token-a")
		resp, err := server.Client().Do(req)
		if err != nil {
			results[i].err = err
			return
		}
		defer resp.Body.Close()
		var decoded map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&decoded)
		results[i].code = resp.StatusCode
		results[i].resp = decoded
	}
	wg.Add(2)
	go post(0, id1, d1)
	go post(1, id2, d2)
	wg.Wait()
	for i, r := range results {
		if r.err != nil {
			t.Fatalf("result %d failed: %v", i, r.err)
		}
		if r.code != http.StatusOK {
			t.Fatalf("result %d status = %d: %v", i, r.code, r.resp)
		}
	}

	// Exactly one winner: one completed, one conflicted.
	statuses := map[string]bool{}
	body := noteBody(t, cfg, 100)
	for _, r := range results {
		status := r.resp["status"].(string)
		statuses[status] = true
		switch status {
		case "completed":
			if !strings.Contains(body, "Proposal") {
				t.Fatalf("winner did not write its proposal: %q", body)
			}
		case "conflict":
			res := r.resp["result"].(map[string]any)
			if res["proposed_body"] == "" {
				t.Fatalf("conflict must preserve the proposal: %v", r.resp)
			}
			if errMsg, _ := r.resp["error"].(string); !strings.Contains(errMsg, "changed since the request was sent") {
				t.Fatalf("conflict reason missing: %q", errMsg)
			}
		default:
			t.Fatalf("unexpected outcome %q: %v", status, r.resp)
		}
	}
	if !statuses["completed"] || !statuses["conflict"] {
		t.Fatalf("the race must yield one completed and one conflict, got %v", statuses)
	}
	// The note holds one complete proposal, not a splice of both.
	if body != "# Proposal 1\n" && body != "# Proposal 2\n" {
		t.Fatalf("note body must be exactly one proposal, got %q", body)
	}
}

// TestUpdateApplyConflictWhenTargetChanged verifies the sequential loser: a second update sent
// against the same ETag that the first already consumed settles to conflict and leaves the winner's
// body untouched.
func TestUpdateApplyConflictWhenTargetChanged(t *testing.T) {
	server, cfg := requestServer(t)
	id1, d1 := createUpdateHTTP(t, server, cfg, "First wins.")
	id2, d2 := createUpdateHTTP(t, server, cfg, "Second loses.")

	submitUpdateResult(t, server, id1, d1, "# Winner\\n", "")
	detail := submitUpdateResult(t, server, id2, d2, "# Loser\\n", "")

	if detail["status"] != "conflict" {
		t.Fatalf("loser must conflict, got %v", detail["status"])
	}
	if got := noteBody(t, cfg, 100); got != "# Winner\n" {
		t.Fatalf("loser must not overwrite the winner: %q", got)
	}
	res := detail["result"].(map[string]any)
	if res["proposed_body"] != "# Loser\n" {
		t.Fatalf("conflict must keep the loser's proposal: %v", res)
	}
	errMsg, _ := detail["error"].(string)
	if !strings.Contains(errMsg, "changed since the request was sent") {
		t.Fatalf("conflict reason missing: %q", errMsg)
	}
	// The conflicted request is retryable, and the old dispatch's report is stale after a retry.
	if code, _ := postRequest(t, server.URL+"/api/requests/"+id2+"/retry", `{}`); code != http.StatusOK {
		t.Fatalf("retry after conflict = %d", code)
	}
	if code, _ := postRequest(t, server.URL+"/api/requests/"+id2+"/result",
		`{"dispatch_id":"`+d2+`","proposed_body":"# Late\n"}`); code != http.StatusConflict {
		t.Fatalf("old dispatch result after retry should 409, got %d", code)
	}
}

func TestUpdateApplyConflictWhenTargetDeleted(t *testing.T) {
	server, cfg := requestServer(t)
	id, dispatch := createUpdateHTTP(t, server, cfg, "Target will vanish.")

	if err := os.Remove(cfg.NotePath(100)); err != nil {
		t.Fatal(err)
	}
	detail := submitUpdateResult(t, server, id, dispatch, "# Ghost\\n", "")
	if detail["status"] != "conflict" {
		t.Fatalf("deleted target must conflict, got %v", detail["status"])
	}
	res := detail["result"].(map[string]any)
	if res["proposed_body"] != "# Ghost\n" {
		t.Fatalf("deleted-target conflict must keep the proposal: %v", res)
	}
	errMsg, _ := detail["error"].(string)
	if !strings.Contains(errMsg, "no longer exists") {
		t.Fatalf("deletion reason missing: %q", errMsg)
	}
}

// TestUpdateApplyOtherVaultSameID verifies that an update applies to the note in the request's own
// vault — ids are vault-local, so a same-numbered note in another vault is never touched.
func TestUpdateApplyOtherVaultSameID(t *testing.T) {
	srv, server, main, work := twoVaultRequestServer(t)
	mainCfg := &config.Config{VaultDir: main, Extensions: []string{".md"}}
	workCfg := &config.Config{VaultDir: work, Extensions: []string{".md"}}
	// twoVaultRequestServer writes the notes without indexing them; the update gate resolves targets
	// through the index, so reconcile both vaults first.
	srv.refresh(srv.active)
	workView, err := srv.viewByName("work")
	if err != nil {
		t.Fatal(err)
	}
	srv.refresh(workView)

	// update drives one full update through the addressed vault: the empty label is the launch
	// vault, "work" the registered one. Both vaults hold a note under id 100.
	update := func(label string, cfg *config.Config, proposal string) {
		t.Helper()
		suffix := ""
		if label == "work" {
			suffix = "?vault=work"
		}
		raw, err := os.ReadFile(cfg.NotePath(100))
		if err != nil {
			t.Fatal(err)
		}
		etag := note.ContentETag(raw)
		code, created := postRequest(t, server.URL+"/api/requests"+suffix,
			`{"intent":"update","instruction":"Update here.","agent_id":"a","update_target":{"note_id":100,"etag":"`+etag+`"}}`)
		if code != http.StatusAccepted {
			t.Fatalf("create in %q = %d: %v", label, code, created)
		}
		id := reqID(t, created)
		dispatch := currentDispatchID(t, created)
		if code, _ := postRequest(t, server.URL+"/api/requests/"+id+"/claim"+suffix, `{"dispatch_id":"`+dispatch+`"}`); code != http.StatusOK {
			t.Fatalf("claim in %q = %d", label, code)
		}
		if code, detail := postRequest(t, server.URL+"/api/requests/"+id+"/result"+suffix,
			`{"dispatch_id":"`+dispatch+`","proposed_body":"`+proposal+`"}`); code != http.StatusOK {
			t.Fatalf("result in %q = %d: %v", label, code, detail)
		}
	}

	// main's note 100 gets its own proposal, and work's note 100 gets work's — each apply stays in
	// the vault that holds the request.
	update("main", mainCfg, "# Main update\\n")
	update("work", workCfg, "# Work update\\n")

	if got := noteBody(t, mainCfg, 100); got != "# Main update\n" {
		t.Fatalf("main note must hold main's proposal, got %q", got)
	}
	if got := noteBody(t, workCfg, 100); got != "# Work update\n" {
		t.Fatalf("work note must hold work's proposal, got %q", got)
	}
}

// TestUpdateApplySameResultReplay verifies the idempotency contract over the HTTP surface: the same
// result re-submitted to a completed update is a 200 replay, and a different result over a settled
// (completed or conflicted) update is a 409.
func TestUpdateApplySameResultReplay(t *testing.T) {
	server, cfg := requestServer(t)
	// Both updates capture the same initial ETag; the first apply consumes it, so the second result
	// must conflict and stay unreplayable.
	id, dispatch := createUpdateHTTP(t, server, cfg, "Make it clearer.")
	id2, d2 := createUpdateHTTP(t, server, cfg, "Loser.")

	submitUpdateResult(t, server, id, dispatch, "# New\\n", "Rationale.")

	code, replay := postRequest(t, server.URL+"/api/requests/"+id+"/result",
		`{"dispatch_id":"`+dispatch+`","proposed_body":"# New\n","answer_markdown":"Rationale."}`)
	if code != http.StatusOK || replay["status"] != "completed" {
		t.Fatalf("same-result replay after completion should 200: %d %v", code, replay)
	}
	if got := noteBody(t, cfg, 100); got != "# New\n" {
		t.Fatalf("replay must not rewrite the note: %q", got)
	}
	if code, _ := postRequest(t, server.URL+"/api/requests/"+id+"/result",
		`{"dispatch_id":"`+dispatch+`","proposed_body":"# Different\n"}`); code != http.StatusConflict {
		t.Fatalf("different result over completed should 409, got %d", code)
	}

	// A conflicted update cannot be re-settled by replaying the identical result either.
	detail := submitUpdateResult(t, server, id2, d2, "# A\\n", "")
	if detail["status"] != "conflict" {
		t.Fatalf("loser must conflict, got %v", detail["status"])
	}
	if code, _ := postRequest(t, server.URL+"/api/requests/"+id2+"/result",
		`{"dispatch_id":"`+d2+`","proposed_body":"# A\n"}`); code != http.StatusConflict {
		t.Fatalf("same-result replay over a conflict should 409, got %d", code)
	}
}

func TestUpdateApplyCancelRefusedAfterSettlement(t *testing.T) {
	server, cfg := requestServer(t)
	id, dispatch := createUpdateHTTP(t, server, cfg, "Make it clearer.")
	submitUpdateResult(t, server, id, dispatch, "# Done\\n", "")

	// Applying is past cancel (the spec refuses cancellation once the apply started), and a settled
	// request is equally uncancellable.
	if code, _ := postRequest(t, server.URL+"/api/requests/"+id+"/cancel", `{}`); code != http.StatusConflict {
		t.Fatalf("cancel after apply should 409, got %d", code)
	}
}

// TestUpdateApplyIndexFailure verifies the apply's durable outcome does not depend on the index: a
// broken index refresh is reported and self-heals later, while the note and the request record
// still agree and the response still settles the request as completed.
func TestUpdateApplyIndexFailure(t *testing.T) {
	cfg := &config.Config{
		VaultDir:          t.TempDir(),
		DBPath:            filepath.Join(t.TempDir(), "index.db"),
		Extensions:        []string{".md"},
		DateFormat:        "2006-01-02",
		JournalDateFormat: "20060102",
		Agents:            map[string]config.AgentConfig{"a": {Token: "token-a"}},
	}
	s, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	addIndexedTestNote(t, cfg, s, 100, "Alpha")
	server := httptest.NewServer(New(cfg, s).Handler())
	t.Cleanup(server.Close)

	id, dispatch := createUpdateHTTP(t, server, cfg, "Make it clearer.")
	// Break the index: the apply must still write the note and settle the request.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	detail := submitUpdateResult(t, server, id, dispatch, "# Resilient\\n", "")
	if detail["status"] != "completed" {
		t.Fatalf("index failure must not un-settle the apply: %v", detail["status"])
	}
	if got := noteBody(t, cfg, 100); got != "# Resilient\n" {
		t.Fatalf("index failure must not stop the note replacement: %q", got)
	}
}

// TestUpdateApplyRestartRecovery simulates a server dying at each durable point of an apply and
// verifies the first access of a fresh server settles the leftover applying request without a
// double write or an overwrite: the note body decides resume, complete, or conflict.
func TestUpdateApplyRestartRecovery(t *testing.T) {
	cases := []struct {
		name   string
		note   func(t *testing.T, cfg *config.Config, proposal string)
		status string
	}{
		{
			// Died before the replacement: the note still holds the send-time original, so the
			// fresh server resumes the apply and replaces it.
			name:   "resume",
			note:   func(t *testing.T, cfg *config.Config, proposal string) {},
			status: "completed",
		},
		{
			// Died between the replacement and the completion record: the note already holds the
			// proposal, so the fresh server completes it without a second write.
			name: "complete",
			note: func(t *testing.T, cfg *config.Config, proposal string) {
				if err := os.WriteFile(cfg.NotePath(100), []byte(proposal), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			status: "completed",
		},
		{
			// Died after an unrelated edit won the race: neither original nor proposal, so the
			// fresh server conflicts without overwriting anything.
			name: "conflict",
			note: func(t *testing.T, cfg *config.Config, proposal string) {
				if err := os.WriteFile(cfg.NotePath(100), []byte("# External edit\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			status: "conflict",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server, cfg := requestServer(t)
			id, dispatch := createUpdateHTTP(t, server, cfg, "Make it clearer.")
			const proposal = "# Recovered\n"

			// Drive the request into applying without the server applying it: a direct store call is
			// the crash point right after the result transition persisted the applying state.
			direct := request.New(cfg)
			if _, err := direct.Result(id, dispatch, request.Result{ProposedBody: proposal}, time.Now()); err != nil {
				t.Fatal(err)
			}
			tc.note(t, cfg, proposal)

			// "Restart": a fresh server over the same vault; its first request-store access recovers.
			s2, err := store.Open(cfg.DBPath)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { s2.Close() })
			srv2 := New(cfg, s2)
			server2 := httptest.NewServer(srv2.Handler())
			t.Cleanup(server2.Close)

			code, detail := getRequest(t, server2.URL+"/api/requests/"+id)
			if code != http.StatusOK {
				t.Fatalf("detail after recovery = %d", code)
			}
			if detail["status"] != tc.status {
				t.Fatalf("recovery status = %v, want %s", detail["status"], tc.status)
			}
			switch tc.name {
			case "resume", "complete":
				if got := noteBody(t, cfg, 100); got != proposal {
					t.Fatalf("recovered note must hold the proposal, got %q", got)
				}
				res := detail["result"].(map[string]any)
				ap, ok := res["apply"].(map[string]any)
				if !ok || ap["after_body"] != proposal || ap["after_etag"] == "" {
					t.Fatalf("recovered apply record wrong: %v", res)
				}
			case "conflict":
				if got := noteBody(t, cfg, 100); got != "# External edit\n" {
					t.Fatalf("recovery must not overwrite the external edit: %q", got)
				}
				res := detail["result"].(map[string]any)
				if res["proposed_body"] != proposal {
					t.Fatalf("recovered conflict must keep the proposal: %v", res)
				}
				if errMsg, _ := detail["error"].(string); !strings.Contains(errMsg, "changed while the request was interrupted") {
					t.Fatalf("recovered conflict reason missing: %q", errMsg)
				}
			}
			// No temp files anywhere after recovery.
			if leftovers := tempLeftovers(t, cfg.NoteDir()); len(leftovers) != 0 {
				t.Fatalf("note dir left temp files after recovery: %v", leftovers)
			}
		})
	}
}

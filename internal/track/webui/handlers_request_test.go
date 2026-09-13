package webui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

// requestServer builds a server over a temp vault with one indexed note (100, "Alpha").
func requestServer(t *testing.T) (*httptest.Server, *config.Config) {
	t.Helper()
	return requestServerAgents(t, map[string]config.AgentConfig{
		"a":                  {Token: "token-a"},
		"research-assistant": {Token: "token-research"},
	})
}

// requestServerAgents is requestServer with an explicit agent registry, so a test can register an
// agmsg connection (or none) per agent.
func requestServerAgents(t *testing.T, agents map[string]config.AgentConfig) (*httptest.Server, *config.Config) {
	t.Helper()
	cfg := &config.Config{
		VaultDir:          t.TempDir(),
		DBPath:            filepath.Join(t.TempDir(), "index.db"),
		Extensions:        []string{".md"},
		DateFormat:        "2006-01-02",
		JournalDateFormat: "20060102",
		Agents:            agents,
	}
	s, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	addIndexedTestNote(t, cfg, s, 100, "Alpha")
	server := httptest.NewServer(New(cfg, s).Handler())
	t.Cleanup(server.Close)
	return server, cfg
}

// postRequest posts a JSON body to url and returns the status and decoded body.
func postRequest(t *testing.T, url, body string) (int, map[string]any) {
	t.Helper()
	return requestJSON(t, http.MethodPost, url, body)
}

func getRequest(t *testing.T, url string) (int, map[string]any) {
	t.Helper()
	return requestJSON(t, http.MethodGet, url, "")
}

func requestJSON(t *testing.T, method, url, body string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer token-a")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	var decoded map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&decoded)
	return resp.StatusCode, decoded
}

func reqID(t *testing.T, resp map[string]any) string {
	t.Helper()
	req, _ := resp["request"].(map[string]any)
	id, _ := req["id"].(string)
	if id == "" {
		t.Fatalf("response carries no request id: %v", resp)
	}
	return id
}

func currentDispatchID(t *testing.T, resp map[string]any) string {
	t.Helper()
	// Create wraps the request under "request"; every other endpoint returns it directly.
	req, ok := resp["request"].(map[string]any)
	if !ok {
		req = resp
	}
	attempts, _ := req["attempts"].([]any)
	last, _ := attempts[len(attempts)-1].(map[string]any)
	id, _ := last["id"].(string)
	if id == "" {
		t.Fatalf("response carries no current dispatch id: %v", resp)
	}
	return id
}

func requestStatus(t *testing.T, url string) string {
	t.Helper()
	code, resp := getRequest(t, url)
	if code != http.StatusOK {
		t.Fatalf("detail status = %d", code)
	}
	status, _ := resp["status"].(string)
	return status
}

func TestRequestCreateListDetail(t *testing.T) {
	server, cfg := requestServer(t)
	body := `{"intent":"explain","instruction":"Explain the failure conditions.","agent_id":"research-assistant",
		"context":{"quote":"two-phase commit","note":{"note_id":100,"title":"Alpha"}}}`
	code, resp := postRequest(t, server.URL+"/api/requests", body)
	if code != http.StatusAccepted {
		t.Fatalf("create status = %d, want 202: %v", code, resp)
	}
	if resp["reused"] != false {
		t.Fatalf("first create must not be reused: %v", resp)
	}
	req := resp["request"].(map[string]any)
	if req["status"] != "queued" || req["intent"] != "explain" || req["instruction"] != "Explain the failure conditions." {
		t.Fatalf("stored request wrong: %v", req)
	}
	if req["vault_path"] != cfg.VaultDir || req["input_fingerprint"] == "" {
		t.Fatalf("request identity missing: %v", req)
	}
	attempts := req["attempts"].([]any)
	if len(attempts) != 1 || attempts[0].(map[string]any)["status"] != "queued" {
		t.Fatalf("new request must have one queued attempt: %v", attempts)
	}
	id := reqID(t, resp)

	// The request file is durable under .track/requests.
	if _, err := os.Stat(filepath.Join(cfg.TrackDir(), "requests", id+".json")); err != nil {
		t.Fatalf("request file missing: %v", err)
	}

	// Listing shows it, newest first.
	code, list := getRequest(t, server.URL+"/api/requests")
	if code != http.StatusOK {
		t.Fatalf("list status = %d", code)
	}
	items := list["requests"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["id"] != id {
		t.Fatalf("list should contain the request: %v", list)
	}
	if list["next_cursor"] != "" {
		t.Fatalf("single item list should have no cursor: %v", list)
	}

	// Detail returns the same stored request.
	code, detail := getRequest(t, server.URL+"/api/requests/"+id)
	if code != http.StatusOK || detail["status"] != "queued" {
		t.Fatalf("detail = %d %v", code, detail)
	}
}

func TestRequestCreateIdempotentAndContentType(t *testing.T) {
	server, _ := requestServer(t)
	base := `{"client_request_id":"click-1","intent":"research","instruction":"Same question.","agent_id":"a"}`
	code, first := postRequest(t, server.URL+"/api/requests", base)
	if code != http.StatusAccepted || first["reused"] != false {
		t.Fatalf("first create: %d %v", code, first)
	}
	// Same key + same input: the stored request returns, nothing new is created.
	code, again := postRequest(t, server.URL+"/api/requests", base)
	if code != http.StatusAccepted || again["reused"] != true || reqID(t, again) != reqID(t, first) {
		t.Fatalf("same key+input should reuse request: %d %v", code, again)
	}
	_, list := getRequest(t, server.URL+"/api/requests")
	if items := list["requests"].([]any); len(items) != 1 {
		t.Fatalf("idempotent create stored %d requests, want 1", len(items))
	}
	// Same key + different input: 409.
	code, conflict := postRequest(t, server.URL+"/api/requests", `{"client_request_id":"click-1","intent":"research","instruction":"Different question.","agent_id":"a"}`)
	if code != http.StatusConflict {
		t.Fatalf("same key+different input should 409, got %d: %v", code, conflict)
	}
	// Non-JSON Content-Type is refused on every create.
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/requests", strings.NewReader(base))
	req.Header.Set("Content-Type", "text/plain")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("text/plain create should 415, got %d", resp.StatusCode)
	}
}

func TestRequestCreateRejectsBadInput(t *testing.T) {
	server, cfg := requestServer(t)
	cases := []struct {
		name string
		body string
		want int
	}{
		{"missing instruction", `{"intent":"explain","agent_id":"a"}`, http.StatusBadRequest},
		{"bad intent", `{"intent":"poison","instruction":"x","agent_id":"a"}`, http.StatusBadRequest},
		{"missing agent", `{"intent":"explain","instruction":"x"}`, http.StatusBadRequest},
		{"unregistered agent", `{"intent":"explain","instruction":"x","agent_id":"unknown"}`, http.StatusBadRequest},
		{"update without target", `{"intent":"update","instruction":"x","agent_id":"a"}`, http.StatusBadRequest},
		{"update target missing note", `{"intent":"update","instruction":"x","agent_id":"a","update_target":{"note_id":999}}`, http.StatusBadRequest},
		{"update target on explain", `{"intent":"explain","instruction":"x","agent_id":"a","update_target":{"note_id":100}}`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, resp := postRequest(t, server.URL+"/api/requests", tc.body)
			if code != tc.want {
				t.Fatalf("status = %d, want %d: %v", code, tc.want, resp)
			}
		})
	}
	// A valid update request against the indexed note is accepted.
	code, resp := postRequest(t, server.URL+"/api/requests",
		`{"intent":"update","instruction":"Make it clearer.","agent_id":"a","update_target":{"note_id":100,"etag":"`+testNoteETag(t, cfg)+`"}}`)
	if code != http.StatusAccepted {
		t.Fatalf("update create = %d: %v", code, resp)
	}
	if code, _ := postRequest(t, server.URL+"/api/requests",
		`{"intent":"update","instruction":"Make it clearer.","agent_id":"a","update_target":{"note_id":100,"etag":"stale"}}`); code != http.StatusConflict {
		t.Fatalf("stale update target should conflict, got %d", code)
	}
}

func testNoteETag(t *testing.T, cfg *config.Config) string {
	t.Helper()
	raw, err := os.ReadFile(cfg.NotePath(100))
	if err != nil {
		t.Fatal(err)
	}
	return note.ContentETag(raw)
}

func TestRequestLifecycle(t *testing.T) {
	server, _ := requestServer(t)
	code, created := postRequest(t, server.URL+"/api/requests",
		`{"intent":"explain","instruction":"Explain X.","agent_id":"a"}`)
	if code != http.StatusAccepted {
		t.Fatalf("create = %d: %v", code, created)
	}
	id := reqID(t, created)
	dispatch := currentDispatchID(t, created)

	// Claim confirms the run; a repeated claim is an idempotent 200.
	code, claimed := postRequest(t, server.URL+"/api/requests/"+id+"/claim", `{"dispatch_id":"`+dispatch+`"}`)
	if code != http.StatusOK || claimed["status"] != "running" {
		t.Fatalf("claim = %d: %v", code, claimed)
	}
	code, _ = postRequest(t, server.URL+"/api/requests/"+id+"/claim", `{"dispatch_id":"`+dispatch+`"}`)
	if code != http.StatusOK {
		t.Fatalf("re-claim should be 200, got %d", code)
	}

	// Result completes the request and is recorded durably.
	code, done := postRequest(t, server.URL+"/api/requests/"+id+"/result",
		`{"dispatch_id":"`+dispatch+`","answer_markdown":"# Answer\n\nX because Y.","sources":["[[Alpha]]"]}`)
	if code != http.StatusOK || done["status"] != "completed" {
		t.Fatalf("result = %d: %v", code, done)
	}
	code, detail := getRequest(t, server.URL+"/api/requests/"+id)
	if code != http.StatusOK {
		t.Fatalf("detail = %d", code)
	}
	res := detail["result"].(map[string]any)
	if res["answer_markdown"] != "# Answer\n\nX because Y." || res["fingerprint"] == "" {
		t.Fatalf("result not stored: %v", detail)
	}

	// The same result re-submitted is an idempotent 200; a different result is a 409.
	code, replay := postRequest(t, server.URL+"/api/requests/"+id+"/result",
		`{"dispatch_id":"`+dispatch+`","answer_markdown":"# Answer\n\nX because Y.","sources":["[[Alpha]]"]}`)
	if code != http.StatusOK {
		t.Fatalf("same result replay should be 200, got %d", code)
	}
	_ = replay
	code, conflict := postRequest(t, server.URL+"/api/requests/"+id+"/result",
		`{"dispatch_id":"`+dispatch+`","answer_markdown":"# Different"}`)
	if code != http.StatusConflict {
		t.Fatalf("different result over completed should 409, got %d: %v", code, conflict)
	}
}

func TestRequestRejectsStaleAndInactiveReports(t *testing.T) {
	server, _ := requestServer(t)
	code, created := postRequest(t, server.URL+"/api/requests", `{"intent":"explain","instruction":"x","agent_id":"a"}`)
	if code != http.StatusAccepted {
		t.Fatalf("create = %d", code)
	}
	id := reqID(t, created)
	dispatch := currentDispatchID(t, created)

	// A result before any claim (request still queued) is a 409, not applied.
	code, resp := postRequest(t, server.URL+"/api/requests/"+id+"/result", `{"dispatch_id":"`+dispatch+`","answer_markdown":"early"}`)
	if code != http.StatusConflict {
		t.Fatalf("result on queued request should 409, got %d: %v", code, resp)
	}
	// An unknown dispatch is a 404.
	code, resp = postRequest(t, server.URL+"/api/requests/"+id+"/claim", `{"dispatch_id":"d-nope"}`)
	if code != http.StatusNotFound {
		t.Fatalf("unknown dispatch claim should 404, got %d: %v", code, resp)
	}
	// A missing dispatch id is a 400.
	code, _ = postRequest(t, server.URL+"/api/requests/"+id+"/claim", `{}`)
	if code != http.StatusBadRequest {
		t.Fatalf("missing dispatch id should 400, got %d", code)
	}
	// An empty result is a 400.
	if code, _ := postRequest(t, server.URL+"/api/requests/"+id+"/claim", `{"dispatch_id":"`+dispatch+`"}`); code != http.StatusOK {
		t.Fatalf("claim = %d", code)
	}
	code, _ = postRequest(t, server.URL+"/api/requests/"+id+"/result", `{"dispatch_id":"`+dispatch+`"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("empty result should 400, got %d", code)
	}
}

func TestRequestCancelAndRetry(t *testing.T) {
	server, _ := requestServer(t)

	// Cancel a queued request; later results are refused.
	code, created := postRequest(t, server.URL+"/api/requests", `{"intent":"explain","instruction":"x","agent_id":"a"}`)
	if code != http.StatusAccepted {
		t.Fatalf("create = %d", code)
	}
	id := reqID(t, created)
	dispatch := currentDispatchID(t, created)
	code, cancelled := postRequest(t, server.URL+"/api/requests/"+id+"/cancel", `{}`)
	if code != http.StatusOK || cancelled["status"] != "cancelled" {
		t.Fatalf("cancel = %d: %v", code, cancelled)
	}
	if code, _ := postRequest(t, server.URL+"/api/requests/"+id+"/result",
		`{"dispatch_id":"`+dispatch+`","answer_markdown":"late"}`); code != http.StatusConflict {
		t.Fatalf("result after cancel should 409, got %d", code)
	}
	if code, _ := postRequest(t, server.URL+"/api/requests/"+id+"/retry", `{}`); code != http.StatusConflict {
		t.Fatalf("retry on a cancelled request should 409, got %d", code)
	}

	// Fail, then retry: a fresh dispatch is minted and the old one's reports are stale.
	code, created = postRequest(t, server.URL+"/api/requests", `{"intent":"explain","instruction":"y","agent_id":"a"}`)
	if code != http.StatusAccepted {
		t.Fatalf("create = %d", code)
	}
	id = reqID(t, created)
	first := currentDispatchID(t, created)
	if code, _ := postRequest(t, server.URL+"/api/requests/"+id+"/claim", `{"dispatch_id":"`+first+`"}`); code != http.StatusOK {
		t.Fatalf("claim = %d", code)
	}
	if code, _ := postRequest(t, server.URL+"/api/requests/"+id+"/fail", `{"dispatch_id":"`+first+`","reason":"agent died"}`); code != http.StatusOK {
		t.Fatalf("fail = %d", code)
	}
	code, retried := postRequest(t, server.URL+"/api/requests/"+id+"/retry", `{}`)
	if code != http.StatusOK || retried["status"] != "queued" {
		t.Fatalf("retry = %d: %v", code, retried)
	}
	attempts := retried["attempts"].([]any)
	if len(attempts) != 2 {
		t.Fatalf("retry should keep both attempts: %v", attempts)
	}
	second := currentDispatchID(t, retried)
	if second == first {
		t.Fatal("retry must mint a new dispatch id")
	}
	// The old dispatch's result cannot settle the new attempt.
	if code, _ := postRequest(t, server.URL+"/api/requests/"+id+"/result",
		`{"dispatch_id":"`+first+`","answer_markdown":"stale"}`); code != http.StatusConflict {
		t.Fatalf("old dispatch result should 409, got %d", code)
	}
	// The new attempt completes normally.
	if code, _ := postRequest(t, server.URL+"/api/requests/"+id+"/claim", `{"dispatch_id":"`+second+`"}`); code != http.StatusOK {
		t.Fatalf("re-claim = %d", code)
	}
	code, done := postRequest(t, server.URL+"/api/requests/"+id+"/result", `{"dispatch_id":"`+second+`","answer_markdown":"fresh"}`)
	if code != http.StatusOK || done["status"] != "completed" {
		t.Fatalf("second result = %d: %v", code, done)
	}
}

func TestRequestNotFound(t *testing.T) {
	server, _ := requestServer(t)
	code, _ := getRequest(t, server.URL+"/api/requests/nope")
	if code != http.StatusNotFound {
		t.Fatalf("unknown detail should 404, got %d", code)
	}
	for _, action := range []string{"cancel", "retry", "claim", "result", "fail"} {
		if code, _ := postRequest(t, server.URL+"/api/requests/nope/"+action, `{}`); code != http.StatusNotFound {
			t.Fatalf("unknown %s should 404, got %d", action, code)
		}
	}
}

func TestRequestReportRequiresAgentToken(t *testing.T) {
	server, _ := requestServer(t)
	code, created := postRequest(t, server.URL+"/api/requests", `{"intent":"explain","instruction":"x","agent_id":"a"}`)
	if code != http.StatusAccepted {
		t.Fatalf("create = %d", code)
	}
	id := reqID(t, created)
	dispatch := currentDispatchID(t, created)

	post := func(token string) int {
		req, err := http.NewRequest(http.MethodPost, server.URL+"/api/requests/"+id+"/claim", strings.NewReader(`{"dispatch_id":"`+dispatch+`"}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if got := post(""); got != http.StatusUnauthorized {
		t.Fatalf("missing report token: got %d", got)
	}
	if got := post("wrong"); got != http.StatusUnauthorized {
		t.Fatalf("wrong report token: got %d", got)
	}
	if got := post("token-a"); got != http.StatusOK {
		t.Fatalf("valid report token: got %d", got)
	}
}

func TestRequestGatewayRejectsNonLoopbackBind(t *testing.T) {
	srv := &Server{bindHost: "0.0.0.0"}
	handler := srv.requestLoopbackOnly(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	req := httptest.NewRequest(http.MethodGet, "/api/requests", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("non-loopback bind should reject request gateway: got %d", resp.Code)
	}
}

func TestRequestListPagination(t *testing.T) {
	server, _ := requestServer(t)
	ids := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		code, resp := postRequest(t, server.URL+"/api/requests",
			fmt.Sprintf(`{"intent":"explain","instruction":"item %d","agent_id":"a"}`, i))
		if code != http.StatusAccepted {
			t.Fatalf("create %d = %d", i, code)
		}
		ids = append(ids, reqID(t, resp))
	}
	// Page 1 of 2, newest first, with a cursor.
	code, page1 := getRequest(t, server.URL+"/api/requests?limit=2")
	if code != http.StatusOK {
		t.Fatalf("list = %d", code)
	}
	items := page1["requests"].([]any)
	if len(items) != 2 || items[0].(map[string]any)["id"] != ids[2] || items[1].(map[string]any)["id"] != ids[1] {
		t.Fatalf("page 1 wrong: %v", page1)
	}
	cursor, _ := page1["next_cursor"].(string)
	if cursor == "" {
		t.Fatalf("page 1 should carry a cursor: %v", page1)
	}
	// Page 2 resumes after it.
	code, page2 := getRequest(t, server.URL+"/api/requests?limit=2&cursor="+cursor)
	if code != http.StatusOK {
		t.Fatalf("list page 2 = %d", code)
	}
	items = page2["requests"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["id"] != ids[0] {
		t.Fatalf("page 2 wrong: %v", page2)
	}
	if page2["next_cursor"] != "" {
		t.Fatalf("last page should have no cursor: %v", page2)
	}
}

// TestRequestEndpointsBehindGuard verifies the new write routes sit behind the existing Host/Origin
// guard like every other workspace API: a rebound Host and a foreign Origin are refused.
func TestRequestEndpointsBehindGuard(t *testing.T) {
	server, _ := requestServer(t)
	base := `{"intent":"explain","instruction":"x","agent_id":"a"}`

	do := func(method, host, origin, body string) int {
		req, err := http.NewRequest(method, server.URL+"/api/requests", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if host != "" {
			req.Host = host
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		res, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if got := do(http.MethodPost, "evil.example:8765", "", base); got != http.StatusForbidden {
		t.Fatalf("rebound Host create: got %d, want 403", got)
	}
	if got := do(http.MethodPost, "", "http://evil.example", base); got != http.StatusForbidden {
		t.Fatalf("cross-origin create: got %d, want 403", got)
	}
	// The same-origin browser and the origin-less CLI client pass through.
	if got := do(http.MethodPost, "", "", base); got == http.StatusForbidden {
		t.Fatalf("origin-less create should pass the guard")
	}
}

// fakeSendScript writes an executable send.sh that appends its argv to logPath (one argument per
// line) and then runs body. The log is the observable record of what the dispatch worker handed the
// script — the argument-safety and no-resend assertions read it.
func fakeSendScript(t *testing.T, logPath, body string) string {
	t.Helper()
	script := filepath.Join(t.TempDir(), "send.sh")
	content := "#!/bin/sh\nprintf '%s\\n' \"$@\" >> \"" + logPath + "\"\n" + body + "\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake send.sh: %v", err)
	}
	return script
}

func sendLineCount(t *testing.T, path string) int {
	t.Helper()
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatalf("read send log: %v", err)
	}
	trimmed := strings.TrimRight(string(raw), "\n")
	if trimmed == "" {
		return 0
	}
	return len(strings.Split(trimmed, "\n"))
}

// waitFor polls cond until it holds or the timeout passes; the dispatch worker runs async, so the
// delivery tests wait on its observable effects rather than sleeping.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(msg)
}

// deliveryStatus reads the current attempt's delivery status from a request detail response.
func deliveryStatus(resp map[string]any) string {
	attempts, _ := resp["attempts"].([]any)
	last, _ := attempts[len(attempts)-1].(map[string]any)
	d, _ := last["delivery"].(string)
	return d
}

// agmsgAgent returns a registered agent wired to a fake send.sh logging to logPath.
func agmsgAgent(t *testing.T, logPath, body string) config.AgentConfig {
	t.Helper()
	return config.AgentConfig{
		Token: "token-a",
		Agmsg: &config.AgmsgConfig{
			SendScript: fakeSendScript(t, logPath, body),
			Team:       "team-a",
			Sender:     "track-web",
			Recipient:  "agent-a",
		},
	}
}

// TestRequestDeliverySentAndFailed verifies the whole delivery loop through the HTTP API: a fresh
// request is handed to send.sh with the plain four-argument form, the outcome is persisted on the
// attempt, and a confirmed delivery failure settles the request as failed through the common path.
func TestRequestDeliverySentAndFailed(t *testing.T) {
	t.Run("sent", func(t *testing.T) {
		log := filepath.Join(t.TempDir(), "send.log")
		server, _ := requestServerAgents(t, map[string]config.AgentConfig{"a": agmsgAgent(t, log, "exit 0")})
		code, created := postRequest(t, server.URL+"/api/requests", `{"intent":"explain","instruction":"x","agent_id":"a"}`)
		if code != http.StatusAccepted {
			t.Fatalf("create = %d: %v", code, created)
		}
		id := reqID(t, created)

		waitFor(t, 2*time.Second, func() bool {
			_, detail := getRequest(t, server.URL+"/api/requests/"+id)
			return deliveryStatus(detail) == "sent"
		}, "delivery did not become sent")
		// The send itself used exactly the connection's four plain arguments.
		lines := sendLineCount(t, log)
		if lines != 4 {
			t.Fatalf("send.sh received %d arguments, want the 4-arg plain form (team sender recipient envelope)", lines)
		}
		_, detail := getRequest(t, server.URL+"/api/requests/"+id)
		if detail["status"] != "queued" {
			t.Fatalf("a successful send must not start the execution: status = %v", detail["status"])
		}
		attempts := detail["attempts"].([]any)
		last := attempts[len(attempts)-1].(map[string]any)
		if last["delivery_note"] != "sent" {
			t.Fatalf("delivery note missing: %v", last)
		}
	})

	t.Run("failed", func(t *testing.T) {
		log := filepath.Join(t.TempDir(), "send.log")
		server, _ := requestServerAgents(t, map[string]config.AgentConfig{"a": agmsgAgent(t, log, "echo 'roster error' >&2\nexit 1")})
		code, created := postRequest(t, server.URL+"/api/requests", `{"intent":"explain","instruction":"x","agent_id":"a"}`)
		if code != http.StatusAccepted {
			t.Fatalf("create = %d: %v", code, created)
		}
		id := reqID(t, created)

		waitFor(t, 2*time.Second, func() bool {
			_, detail := getRequest(t, server.URL+"/api/requests/"+id)
			return detail["status"] == "failed"
		}, "confirmed delivery failure did not settle the request")
		_, detail := getRequest(t, server.URL+"/api/requests/"+id)
		if deliveryStatus(detail) != "failed" {
			t.Fatalf("delivery status = %q, want failed", deliveryStatus(detail))
		}
		errMsg, _ := detail["error"].(string)
		if !strings.Contains(errMsg, "delivery failed") || !strings.Contains(errMsg, "roster error") {
			t.Fatalf("failure should carry the delivery reason: %q", errMsg)
		}
	})
}

// TestRequestDeliveryTimeoutStaysQueued verifies that a send that exceeds the deadline is recorded
// as unknown — a timeout is not proof of non-delivery — and the request stays queued for a claim.
func TestRequestDeliveryTimeoutStaysQueued(t *testing.T) {
	old := sendTimeout
	sendTimeout = 150 * time.Millisecond
	t.Cleanup(func() { sendTimeout = old })

	log := filepath.Join(t.TempDir(), "send.log")
	server, _ := requestServerAgents(t, map[string]config.AgentConfig{"a": agmsgAgent(t, log, "exec sleep 30")})
	code, created := postRequest(t, server.URL+"/api/requests", `{"intent":"explain","instruction":"x","agent_id":"a"}`)
	if code != http.StatusAccepted {
		t.Fatalf("create = %d: %v", code, created)
	}
	id := reqID(t, created)

	waitFor(t, 3*time.Second, func() bool {
		_, detail := getRequest(t, server.URL+"/api/requests/"+id)
		return deliveryStatus(detail) == "unknown"
	}, "timed-out send did not record delivery=unknown")
	_, detail := getRequest(t, server.URL+"/api/requests/"+id)
	if detail["status"] != "queued" {
		t.Fatalf("a timeout is not a failure: status = %v", detail["status"])
	}
	if errMsg, _ := detail["error"].(string); errMsg != "" {
		t.Fatalf("timed-out delivery must not settle the request with an error: %q", errMsg)
	}
}

// TestRequestIdempotentCreateDoesNotResend verifies that replaying a create with the same
// client_request_id and input returns the stored request without dispatching a second delivery.
func TestRequestIdempotentCreateDoesNotResend(t *testing.T) {
	log := filepath.Join(t.TempDir(), "send.log")
	server, _ := requestServerAgents(t, map[string]config.AgentConfig{"a": agmsgAgent(t, log, "exit 0")})
	base := `{"client_request_id":"click-1","intent":"explain","instruction":"x","agent_id":"a"}`
	code, first := postRequest(t, server.URL+"/api/requests", base)
	if code != http.StatusAccepted || first["reused"] != false {
		t.Fatalf("first create: %d %v", code, first)
	}
	waitFor(t, 2*time.Second, func() bool { return sendLineCount(t, log) == 4 }, "first delivery did not send")

	// Same key + same input: reused, and no second delivery. A stray resend would bump the log to 8
	// lines; poll long enough for one to arrive, failing the instant it does.
	code, again := postRequest(t, server.URL+"/api/requests", base)
	if code != http.StatusAccepted || again["reused"] != true || reqID(t, again) != reqID(t, first) {
		t.Fatalf("replayed create should reuse: %d %v", code, again)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if n := sendLineCount(t, log); n != 4 {
			t.Fatalf("idempotent create resent the delivery: send.log has %d lines, want 4", n)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// completeRequestHTTP drives a request through create -> claim -> result over the HTTP API and
// returns its id. The answer is the one saved-note tests read back.
func completeRequestHTTP(t *testing.T, server *httptest.Server, body string) string {
	t.Helper()
	code, created := postRequest(t, server.URL+"/api/requests", body)
	if code != http.StatusAccepted {
		t.Fatalf("create = %d: %v", code, created)
	}
	id := reqID(t, created)
	dispatch := currentDispatchID(t, created)
	if code, _ := postRequest(t, server.URL+"/api/requests/"+id+"/claim", `{"dispatch_id":"`+dispatch+`"}`); code != http.StatusOK {
		t.Fatalf("claim = %d", code)
	}
	if code, _ := postRequest(t, server.URL+"/api/requests/"+id+"/result",
		`{"dispatch_id":"`+dispatch+`","answer_markdown":"# The answer\n\nX because Y."}`); code != http.StatusOK {
		t.Fatalf("result = %d", code)
	}
	return id
}

// noteFileCount counts the regular note files under a vault's note directory.
func noteFileCount(t *testing.T, cfg *config.Config) int {
	t.Helper()
	entries, err := os.ReadDir(cfg.NoteDir())
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			n++
		}
	}
	return n
}

func TestRequestSaveHappyPathAndIdempotency(t *testing.T) {
	server, cfg := requestServer(t)
	id := completeRequestHTTP(t, server, `{"intent":"explain","instruction":"Explain X.","agent_id":"a"}`)

	code, saved := postRequest(t, server.URL+"/api/requests/"+id+"/save",
		`{"title":"The answer","client_request_id":"save-1"}`)
	if code != http.StatusOK {
		t.Fatalf("save = %d: %v", code, saved)
	}
	res, ok := saved["result"].(map[string]any)
	if !ok {
		t.Fatalf("save response carries no result: %v", saved)
	}
	sv, ok := res["saved"].(map[string]any)
	if !ok {
		t.Fatalf("save response carries no result.saved: %v", saved)
	}
	noteID := int64(sv["note_id"].(float64))
	if noteID <= 0 || sv["title"] != "The answer" || sv["status"] != "saved" || sv["saved_at"] == "" {
		t.Fatalf("saved record wrong: %v", sv)
	}
	if vault, _ := sv["vault"].(string); vault != "" {
		t.Fatalf("saving into the request's own vault must leave it unlabeled, got %q", vault)
	}
	// The note exists with the answer as its body, indexed under the title.
	raw, err := os.ReadFile(cfg.NotePath(noteID))
	if err != nil {
		t.Fatalf("saved note file missing: %v", err)
	}
	if !strings.Contains(string(raw), "X because Y.") {
		t.Fatalf("saved note body must be the answer: %q", raw)
	}
	// The saved note is indexed: the title resolves to it.
	code, resolved := getRequest(t, server.URL+"/api/resolve?term=The+answer")
	if code != http.StatusOK || resolved["found"] != true {
		t.Fatalf("saved note must resolve by title: %d %v", code, resolved)
	}

	// Resend of the same title returns the same note without creating a second one.
	code, again := postRequest(t, server.URL+"/api/requests/"+id+"/save", `{"title":"The answer","client_request_id":"save-1"}`)
	if code != http.StatusOK {
		t.Fatalf("resend = %d: %v", code, again)
	}
	sv2 := again["result"].(map[string]any)["saved"].(map[string]any)
	if int64(sv2["note_id"].(float64)) != noteID {
		t.Fatalf("resend must return the same note: %v vs %v", sv2, sv)
	}
	if n := noteFileCount(t, cfg); n != 2 { // the fixture's 100.md plus the saved note
		t.Fatalf("resend duplicated the note: %d note files, want 2", n)
	}

	// A different title over the saved request is a 409.
	code, conflict := postRequest(t, server.URL+"/api/requests/"+id+"/save", `{"title":"Rewritten"}`)
	if code != http.StatusConflict {
		t.Fatalf("different-title resave should 409, got %d: %v", code, conflict)
	}

	// The save is durable: the stored request file carries result.saved.
	stored, err := os.ReadFile(filepath.Join(cfg.TrackDir(), "requests", id+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(stored, []byte(`"saved"`)) {
		t.Fatalf("stored request must carry the save record: %s", stored)
	}
}

func TestRequestSaveGuards(t *testing.T) {
	server, cfg := requestServer(t)

	// Unknown request is a 404.
	if code, _ := postRequest(t, server.URL+"/api/requests/nope/save", `{"title":"x"}`); code != http.StatusNotFound {
		t.Fatalf("unknown request save should 404, got %d", code)
	}

	// A queued request (nothing claimed or completed) is not saveable: 409.
	code, created := postRequest(t, server.URL+"/api/requests", `{"intent":"explain","instruction":"x","agent_id":"a"}`)
	if code != http.StatusAccepted {
		t.Fatalf("create = %d", code)
	}
	id := reqID(t, created)
	if code, _ := postRequest(t, server.URL+"/api/requests/"+id+"/save", `{}`); code != http.StatusBadRequest {
		t.Fatalf("missing title should 400, got %d", code)
	}
	if code, _ := postRequest(t, server.URL+"/api/requests/"+id+"/save", `{"title":"x"}`); code != http.StatusConflict {
		t.Fatalf("save on a queued request should 409, got %d", code)
	}

	// A completed update request (proposed body, no answer) is not savable: 409.
	code, upd := postRequest(t, server.URL+"/api/requests",
		`{"intent":"update","instruction":"Make it clearer.","agent_id":"a","update_target":{"note_id":100,"etag":"`+testNoteETag(t, cfg)+`"}}`)
	if code != http.StatusAccepted {
		t.Fatalf("update create = %d: %v", code, upd)
	}
	updID := reqID(t, upd)
	updDispatch := currentDispatchID(t, upd)
	if code, _ := postRequest(t, server.URL+"/api/requests/"+updID+"/claim", `{"dispatch_id":"`+updDispatch+`"}`); code != http.StatusOK {
		t.Fatalf("claim = %d", code)
	}
	if code, _ := postRequest(t, server.URL+"/api/requests/"+updID+"/result",
		`{"dispatch_id":"`+updDispatch+`","proposed_body":"# New body\n"}`); code != http.StatusOK {
		t.Fatalf("update result = %d", code)
	}
	if code, _ := postRequest(t, server.URL+"/api/requests/"+updID+"/save", `{"title":"x"}`); code != http.StatusConflict {
		t.Fatalf("save on an update request should 409, got %d", code)
	}

	// A title that already resolves is a collision, never an overwrite: 409.
	other := completeRequestHTTP(t, server, `{"intent":"explain","instruction":"Explain Y.","agent_id":"a"}`)
	if code, _ := postRequest(t, server.URL+"/api/requests/"+other+"/save", `{"title":"Alpha"}`); code != http.StatusConflict {
		t.Fatalf("collision title should 409, got %d", code)
	}
	// The collision leaves no note behind.
	if n := noteFileCount(t, cfg); n != 1 {
		t.Fatalf("collision must not create a note: %d note files, want 1", n)
	}

	// An unknown target vault is refused, never silently falling back: 400.
	if code, _ := postRequest(t, server.URL+"/api/requests/"+other+"/save", `{"title":"Fine title","vault":"nope"}`); code != http.StatusBadRequest {
		t.Fatalf("unknown target vault should 400, got %d", code)
	}
	// The non-JSON Content-Type rule applies to save too: 415.
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/requests/"+other+"/save", strings.NewReader(`{"title":"x"}`))
	req.Header.Set("Content-Type", "text/plain")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("text/plain save should 415, got %d", resp.StatusCode)
	}
}

// twoVaultRequestServer starts a workspace whose launch vault is registered as "main" and which also
// serves a registered "work" vault, with one registered agent so requests can be created. It returns
// the Server behind the workspace (for opening vault views directly), the HTTP server, and the two
// vault directories.
func twoVaultRequestServer(t *testing.T) (*Server, *httptest.Server, string, string) {
	t.Helper()
	main, work := t.TempDir(), t.TempDir()
	writeVaultNote(t, main, 100, "Alpha", "# Alpha\n")
	writeVaultNote(t, work, 100, "Worknote", "# Worknote\n")

	configPath := filepath.Join(t.TempDir(), "config.yml")
	body := "cache_dir: " + t.TempDir() + "\nvaults:\n  main: " + main + "\n  work: " + work + "\n" +
		"agents:\n  a:\n    token: token-a\n"
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TRACK_CONFIG", configPath)
	t.Setenv("TRACK_VAULT", main)
	t.Setenv("TRACK_CACHE_DIR", "")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	s, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	srv := New(cfg, s)
	t.Cleanup(srv.closeViews)
	server := httptest.NewServer(srv.Handler())
	t.Cleanup(server.Close)
	return srv, server, main, work
}

func TestRequestSaveCrossVault(t *testing.T) {
	_, server, main, work := twoVaultRequestServer(t)
	id := completeRequestHTTP(t, server, `{"intent":"explain","instruction":"Explain Z.","agent_id":"a"}`)

	// Save into the registered "work" vault by name.
	code, saved := postRequest(t, server.URL+"/api/requests/"+id+"/save", `{"title":"Cross vault answer","vault":"work"}`)
	if code != http.StatusOK {
		t.Fatalf("cross-vault save = %d: %v", code, saved)
	}
	sv := saved["result"].(map[string]any)["saved"].(map[string]any)
	if sv["vault"] != "work" {
		t.Fatalf("saved note must name the target vault, got %v", sv)
	}
	noteID := int64(sv["note_id"].(float64))
	workCfg := &config.Config{VaultDir: work, Extensions: []string{".md"}}
	if _, err := os.Stat(workCfg.NotePath(noteID)); err != nil {
		t.Fatalf("note must land in the named vault: %v", err)
	}
	// The note is indexed in the target vault: the title resolves there, and only there.
	code, resolved := getRequest(t, server.URL+"/api/resolve?term=Cross+vault+answer&vault=work")
	if code != http.StatusOK || resolved["found"] != true {
		t.Fatalf("saved note must resolve in the target vault: %d %v", code, resolved)
	}
	code, resolved = getRequest(t, server.URL+"/api/resolve?term=Cross+vault+answer")
	if code != http.StatusOK || resolved["found"] != false {
		t.Fatalf("saved note must not resolve in the launch vault: %d %v", code, resolved)
	}

	// The resend with the same title+vault returns the same note.
	code, again := postRequest(t, server.URL+"/api/requests/"+id+"/save", `{"title":"Cross vault answer","vault":"work"}`)
	if code != http.StatusOK {
		t.Fatalf("resend = %d: %v", code, again)
	}
	sv2 := again["result"].(map[string]any)["saved"].(map[string]any)
	if int64(sv2["note_id"].(float64)) != noteID {
		t.Fatalf("resend must return the same note")
	}

	// The same title aimed at a different vault is already saved elsewhere: 409.
	if code, _ := postRequest(t, server.URL+"/api/requests/"+id+"/save", `{"title":"Cross vault answer"}`); code != http.StatusConflict {
		t.Fatalf("same title different vault should 409, got %d", code)
	}

	// A second request can save its own answer into the same vault under a different title.
	other := completeRequestHTTP(t, server, `{"intent":"research","instruction":"Research W.","agent_id":"a"}`)
	if code, _ := postRequest(t, server.URL+"/api/requests/"+other+"/save", `{"title":"Second answer","vault":"work"}`); code != http.StatusOK {
		t.Fatalf("second save = %d", code)
	}

	// A save whose body names the launch vault by its registry name lands there, unlabeled.
	third := completeRequestHTTP(t, server, `{"intent":"explain","instruction":"Explain V.","agent_id":"a"}`)
	code, named := postRequest(t, server.URL+"/api/requests/"+third+"/save", `{"title":"Named launch save","vault":"main"}`)
	if code != http.StatusOK {
		t.Fatalf("named launch vault save = %d: %v", code, named)
	}
	svNamed := named["result"].(map[string]any)["saved"].(map[string]any)
	if vault, _ := svNamed["vault"].(string); vault != "" {
		t.Fatalf("reaching the launch vault by name must still leave the save unlabeled, got %q", vault)
	}
	// The note landed in the launch vault's own directory with the answer as its body. (Note ids are
	// vault-local and both vaults allocate from the same time bucket, so the id alone cannot tell the
	// vaults apart — the file location and content can.)
	mainCfg := &config.Config{VaultDir: main, Extensions: []string{".md"}}
	namedNoteID := int64(svNamed["note_id"].(float64))
	rawNamed, err := os.ReadFile(mainCfg.NotePath(namedNoteID))
	if err != nil {
		t.Fatalf("named launch vault save must land in the launch vault: %v", err)
	}
	if !strings.Contains(string(rawNamed), "X because Y.") {
		t.Fatalf("named launch vault note must carry the answer: %q", rawNamed)
	}
}

func TestRequestFollowUpSeedsParentContext(t *testing.T) {
	server, _ := requestServer(t)
	parentID := completeRequestHTTP(t, server, `{"intent":"research","instruction":"Is X the bottleneck?","agent_id":"a"}`)

	// The follow-up names its parent and carries the parent's settled answer as context.
	code, child := postRequest(t, server.URL+"/api/requests",
		`{"client_request_id":"child-1","parent_request_id":"`+parentID+`","intent":"research","instruction":"What about Y?","agent_id":"a"}`)
	if code != http.StatusAccepted {
		t.Fatalf("follow-up create = %d: %v", code, child)
	}
	req := child["request"].(map[string]any)
	if req["parent_request_id"] != parentID {
		t.Fatalf("follow-up must record its parent: %v", req)
	}
	ctx := req["context"].(map[string]any)
	prior, ok := ctx["prior_answers"].([]any)
	if !ok || len(prior) != 1 {
		t.Fatalf("follow-up must carry one prior answer: %v", ctx)
	}
	pa := prior[0].(map[string]any)
	if pa["request_id"] != parentID || pa["instruction"] != "Is X the bottleneck?" || pa["answer_markdown"] != "# The answer\n\nX because Y." {
		t.Fatalf("prior answer wrong: %v", pa)
	}

	// The parent is unchanged: no prior answers of its own, its result intact.
	code, parent := getRequest(t, server.URL+"/api/requests/"+parentID)
	if code != http.StatusOK {
		t.Fatalf("parent detail = %d", code)
	}
	parentCtx := parent["context"].(map[string]any)
	if _, ok := parentCtx["prior_answers"]; ok {
		t.Fatalf("parent must not grow prior answers: %v", parentCtx)
	}
	if parent["result"].(map[string]any)["answer_markdown"] != "# The answer\n\nX because Y." {
		t.Fatalf("parent result must be untouched")
	}

	// An unknown parent is refused.
	if code, _ := postRequest(t, server.URL+"/api/requests",
		`{"parent_request_id":"req-nope","intent":"research","instruction":"x","agent_id":"a"}`); code != http.StatusBadRequest {
		t.Fatalf("unknown parent should 400, got %d", code)
	}

	// An idempotent replay of the follow-up create reuses the child.
	code, again := postRequest(t, server.URL+"/api/requests",
		`{"client_request_id":"child-1","parent_request_id":"`+parentID+`","intent":"research","instruction":"What about Y?","agent_id":"a"}`)
	if code != http.StatusAccepted || again["reused"] != true || reqID(t, again) != reqID(t, child) {
		t.Fatalf("follow-up replay should reuse: %d %v", code, again)
	}
}

// TestRequestJSONKeepsAgmsgConfigOut verifies the requirement that agmsg connection settings stay
// machine-local: neither the request JSON model, the stored request file, nor the web API responses
// may carry the connection config or the agent token (docs/spec/live-agent-requests.md).
func TestRequestJSONKeepsAgmsgConfigOut(t *testing.T) {
	log := filepath.Join(t.TempDir(), "send.log")
	server, cfg := requestServerAgents(t, map[string]config.AgentConfig{"a": agmsgAgent(t, log, "exit 0")})
	code, created := postRequest(t, server.URL+"/api/requests", `{"intent":"explain","instruction":"x","agent_id":"a"}`)
	if code != http.StatusAccepted {
		t.Fatalf("create = %d: %v", code, created)
	}
	id := reqID(t, created)

	// Every agmsg connection key and the token are machine config only.
	forbidden := []string{"agmsg", "send_script", "team", "sender", "recipient", "token"}
	assertForbidden := func(prefix string, raw []byte) {
		t.Helper()
		for _, key := range forbidden {
			if bytes.Contains(raw, []byte(`"`+key+`"`)) {
				t.Fatalf("%s leaks the connection key %q: %s", prefix, key, raw)
			}
		}
	}
	req, _ := created["request"].(map[string]any)
	raw, _ := json.Marshal(req)
	assertForbidden("create response", raw)

	_, detail := getRequest(t, server.URL+"/api/requests/"+id)
	raw, _ = json.Marshal(detail)
	assertForbidden("detail response", raw)

	stored, err := os.ReadFile(filepath.Join(cfg.TrackDir(), "requests", id+".json"))
	if err != nil {
		t.Fatalf("read stored request: %v", err)
	}
	assertForbidden("stored request file", stored)
}

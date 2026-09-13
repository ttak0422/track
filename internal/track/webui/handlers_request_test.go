package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

// requestServer builds a server over a temp vault with one indexed note (100, "Alpha").
func requestServer(t *testing.T) (*httptest.Server, *config.Config) {
	t.Helper()
	cfg := &config.Config{
		VaultDir:          t.TempDir(),
		DBPath:            filepath.Join(t.TempDir(), "index.db"),
		Extensions:        []string{".md"},
		DateFormat:        "2006-01-02",
		JournalDateFormat: "20060102",
		Agents: map[string]config.AgentConfig{
			"a":                  {Token: "token-a"},
			"research-assistant": {Token: "token-research"},
		},
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

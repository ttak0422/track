package request

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/config"
)

func newTestStore(t *testing.T) (*Store, *config.Config) {
	t.Helper()
	cfg := &config.Config{
		VaultDir:          t.TempDir(),
		DBPath:            filepath.Join(t.TempDir(), "index.db"),
		Extensions:        []string{".md"},
		DateFormat:        "2006-01-02",
		JournalDateFormat: "20060102",
	}
	return New(cfg), cfg
}

func at(offset time.Duration) time.Time {
	return time.Unix(1_700_000_000, 0).Add(offset)
}

func rejectReason(t *testing.T, err error) RejectReason {
	t.Helper()
	var re *Error
	if !errors.As(err, &re) {
		t.Fatalf("want a request.Error, got %v", err)
	}
	return re.Reason
}

func TestCreatePersistsAndReloads(t *testing.T) {
	st, cfg := newTestStore(t)
	now := at(0)
	res, err := st.Create(NewRequest{
		Intent:      IntentExplain,
		Instruction: "Explain the two-phase commit failure conditions.",
		AgentID:     "research-assistant",
		Context:     Context{Quote: "In two-phase commit…"},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Reused {
		t.Fatal("first create must not be a reuse")
	}
	r := res.Request
	if r.Status != StatusQueued || len(r.Attempts) != 1 || r.Attempts[0].Status != AttemptQueued {
		t.Fatalf("new request must be queued with one queued attempt: %+v", r)
	}
	if r.Version != CurrentVersion || r.ID == "" || r.VaultPath != cfg.VaultDir {
		t.Fatalf("request identity wrong: %+v", r)
	}
	if r.InputFingerprint == "" || r.Result != nil {
		t.Fatalf("fingerprint or result wrong: %+v", r)
	}
	// The file landed under .track/requests/<id>.json, atomically, with no temp leftovers.
	if _, err := os.Stat(filepath.Join(cfg.TrackDir(), RequestsDirName, r.ID+".json")); err != nil {
		t.Fatalf("request file missing: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(cfg.TrackDir(), RequestsDirName))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") || strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("temp file leaked into requests dir: %s", e.Name())
		}
	}
	// A fresh store over the same vault reads the same request back.
	reopened, err := New(cfg).Get(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.ID != r.ID || reopened.Instruction != "Explain the two-phase commit failure conditions." || reopened.Status != StatusQueued {
		t.Fatalf("reloaded request differs: %+v", reopened)
	}
}

func TestCreateIdempotentClientRequestID(t *testing.T) {
	st, _ := newTestStore(t)
	base := NewRequest{
		ClientRequestID: "click-1",
		Intent:          IntentResearch,
		Instruction:     "Is the network the bottleneck?",
		AgentID:         "research-assistant",
	}
	first, err := st.Create(base, at(0))
	if err != nil {
		t.Fatal(err)
	}
	// Same key, same input: the existing request comes back, nothing new is stored.
	again, err := st.Create(base, at(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !again.Reused || again.Request.ID != first.Request.ID {
		t.Fatalf("same key+input should reuse request %s, got %+v", first.Request.ID, again)
	}
	if got := len(listIDs(t, st)); got != 1 {
		t.Fatalf("idempotent create stored %d requests, want 1", got)
	}
	// Same key, different input: refused, and the original is untouched.
	different := base
	different.Instruction = "Is the memory the bottleneck?"
	if _, err := st.Create(different, at(2*time.Second)); rejectReason(t, err) != RejectInputConflict {
		t.Fatalf("different input under the same key should be input_conflict, got %v", err)
	}
	if got := len(listIDs(t, st)); got != 1 {
		t.Fatalf("conflicting create stored %d requests, want 1", got)
	}
	// Without a key every call creates a distinct request.
	base.ClientRequestID = ""
	second, err := st.Create(base, at(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if second.Request.ID == first.Request.ID {
		t.Fatalf("keyless creates should be distinct")
	}
	if got := len(listIDs(t, st)); got != 2 {
		t.Fatalf("keyless creates stored %d requests, want 2", got)
	}
}

func listIDs(t *testing.T, st *Store) []string {
	t.Helper()
	reqs, _, err := st.List(100, "")
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(reqs))
	for _, r := range reqs {
		ids = append(ids, r.ID)
	}
	return ids
}

func TestCreateValidation(t *testing.T) {
	st, _ := newTestStore(t)
	cases := []struct {
		name string
		in   NewRequest
		want RejectReason
	}{
		{"bad intent", NewRequest{Intent: "poison", Instruction: "x", AgentID: "a"}, RejectInvalidRequest},
		{"empty instruction", NewRequest{Intent: IntentExplain, AgentID: "a"}, RejectInvalidRequest},
		{"blank instruction", NewRequest{Intent: IntentExplain, Instruction: "  ", AgentID: "a"}, RejectInvalidRequest},
		{"empty agent", NewRequest{Intent: IntentExplain, Instruction: "x"}, RejectInvalidRequest},
		{"update without target", NewRequest{Intent: IntentUpdate, Instruction: "x", AgentID: "a"}, RejectInvalidRequest},
		{"update target without id", NewRequest{Intent: IntentUpdate, Instruction: "x", AgentID: "a", UpdateTarget: &NoteRef{}}, RejectInvalidRequest},
		{"target on explain", NewRequest{Intent: IntentExplain, Instruction: "x", AgentID: "a", UpdateTarget: &NoteRef{NoteID: 1}}, RejectInvalidRequest},
		{"oversize instruction", NewRequest{Intent: IntentExplain, Instruction: strings.Repeat("i", MaxInstructionBytes+1), AgentID: "a"}, RejectOversize},
		{"oversize quote", NewRequest{Intent: IntentExplain, Instruction: "x", AgentID: "a", Context: Context{Quote: strings.Repeat("q", MaxQuoteBytes+1)}}, RejectOversize},
		{"oversize note body", NewRequest{Intent: IntentExplain, Instruction: "x", AgentID: "a", Context: Context{Note: &NoteRef{NoteID: 5, Body: strings.Repeat("b", MaxNoteBodyBytes+1)}}}, RejectOversize},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := st.Create(tc.in, at(0)); rejectReason(t, err) != tc.want {
				t.Fatalf("reason = %v, want %s", err, tc.want)
			}
		})
	}
	if got := len(listIDs(t, st)); got != 0 {
		t.Fatalf("rejected creates must store nothing, got %d requests", got)
	}
}

func TestClaim(t *testing.T) {
	st, _ := newTestStore(t)
	r, err := st.Create(NewRequest{Intent: IntentExplain, Instruction: "go", AgentID: "a"}, at(0))
	if err != nil {
		t.Fatal(err)
	}
	d := r.Request.currentDispatch().ID

	// Happy path: queued -> running.
	claimed, err := st.Claim(r.Request.ID, d, at(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if claimed.Idempotent || claimed.Request.Status != StatusRunning || claimed.Request.currentDispatch().Status != AttemptRunning {
		t.Fatalf("claim did not start the run: %+v", claimed.Request)
	}

	// A repeated claim of the same current attempt is an idempotent success (response-loss retry).
	again, err := st.Claim(r.Request.ID, d, at(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !again.Idempotent || again.Request.Status != StatusRunning {
		t.Fatalf("re-claim should be an idempotent success: %+v", again)
	}

	// Unknown request and unknown dispatch.
	if _, err := st.Claim("nope", d, at(0)); rejectReason(t, err) != RejectUnknownRequest {
		t.Fatalf("unknown request should be unknown_request, got %v", err)
	}
	if _, err := st.Claim(r.Request.ID, "d-1", at(0)); rejectReason(t, err) != RejectUnknownDispatch {
		t.Fatalf("unknown dispatch should be unknown_dispatch, got %v", err)
	}
	// Missing dispatch id is a client error.
	if _, err := st.Claim(r.Request.ID, "", at(0)); rejectReason(t, err) != RejectInvalidRequest {
		t.Fatalf("missing dispatch id should be invalid_request, got %v", err)
	}
}

func TestResultHappyPathAndIdempotency(t *testing.T) {
	st, _ := newTestStore(t)
	r, err := st.Create(NewRequest{Intent: IntentExplain, Instruction: "explain", AgentID: "a"}, at(0))
	if err != nil {
		t.Fatal(err)
	}
	d := r.Request.currentDispatch().ID
	if _, err := st.Claim(r.Request.ID, d, at(time.Second)); err != nil {
		t.Fatal(err)
	}
	res := Result{AnswerMarkdown: "# The answer\n\nTwo-phase commit…"}
	settled, err := st.Result(r.Request.ID, d, res, at(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if settled.Request.Status != StatusCompleted || settled.Request.Result == nil ||
		settled.Request.Result.AnswerMarkdown != res.AnswerMarkdown || settled.Request.Result.Fingerprint == "" {
		t.Fatalf("result did not complete the request: %+v", settled.Request)
	}
	if settled.Request.currentDispatch().Status != AttemptCompleted {
		t.Fatalf("attempt should be completed: %+v", settled.Request.currentDispatch())
	}

	// The same result re-submitted is an idempotent success returning the stored request.
	replayed, err := st.Result(r.Request.ID, d, res, at(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !replayed.Idempotent || replayed.Request.ID != r.Request.ID {
		t.Fatalf("same result re-submission should be idempotent: %+v", replayed)
	}

	// A different result over the completed request is refused.
	if _, err := st.Result(r.Request.ID, d, Result{AnswerMarkdown: "different"}, at(4*time.Second)); rejectReason(t, err) != RejectResultConflict {
		t.Fatalf("different result over completed should be result_conflict, got %v", err)
	}
}

func TestResultGuards(t *testing.T) {
	st, _ := newTestStore(t)
	r, err := st.Create(NewRequest{Intent: IntentExplain, Instruction: "x", AgentID: "a"}, at(0))
	if err != nil {
		t.Fatal(err)
	}
	d := r.Request.currentDispatch().ID

	// Result before claim (request still queued): inactive.
	if _, err := st.Result(r.Request.ID, d, Result{AnswerMarkdown: "x"}, at(time.Second)); rejectReason(t, err) != RejectInactiveDispatch {
		t.Fatalf("result on a queued request should be inactive, got %v", err)
	}
	// Empty result: invalid.
	if _, err := st.Result(r.Request.ID, d, Result{}, at(time.Second)); rejectReason(t, err) != RejectInvalidRequest {
		t.Fatalf("empty result should be invalid_request, got %v", err)
	}
	// Oversize answer: refused.
	if _, err := st.Result(r.Request.ID, d, Result{AnswerMarkdown: strings.Repeat("a", MaxAnswerBytes+1)}, at(time.Second)); rejectReason(t, err) != RejectOversize {
		t.Fatalf("oversize answer should be oversize, got %v", err)
	}
	if _, err := st.Result(r.Request.ID, "d-1", Result{AnswerMarkdown: "x"}, at(time.Second)); rejectReason(t, err) != RejectUnknownDispatch {
		t.Fatalf("unknown dispatch result should be unknown_dispatch, got %v", err)
	}

	if _, err := st.Claim(r.Request.ID, d, at(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Fail(r.Request.ID, d, "the agent died", at(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	// Result after failure: inactive.
	if _, err := st.Result(r.Request.ID, d, Result{AnswerMarkdown: "x"}, at(4*time.Second)); rejectReason(t, err) != RejectInactiveDispatch {
		t.Fatalf("result on a failed request should be inactive, got %v", err)
	}
}

func TestUpdateResultRecordsProposedBody(t *testing.T) {
	st, _ := newTestStore(t)
	target := &NoteRef{NoteID: 42, ETag: "abc123"}
	r, err := st.Create(NewRequest{
		Intent:       IntentUpdate,
		Instruction:  "Make the intro clearer.",
		AgentID:      "a",
		UpdateTarget: target,
	}, at(0))
	if err != nil {
		t.Fatal(err)
	}
	d := r.Request.currentDispatch().ID
	if _, err := st.Claim(r.Request.ID, d, at(time.Second)); err != nil {
		t.Fatal(err)
	}
	// Stage 1: the proposed body is recorded and the request completes; the server-side apply
	// (running -> applying -> completed/conflict) is the update-apply stage.
	settled, err := st.Result(r.Request.ID, d, Result{ProposedBody: "# New body\n"}, at(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if settled.Request.Status != StatusCompleted || settled.Request.Result.ProposedBody != "# New body\n" {
		t.Fatalf("update result not recorded: %+v", settled.Request)
	}
	// An update result without a proposed body is invalid.
	r2, err := st.Create(NewRequest{Intent: IntentUpdate, Instruction: "x", AgentID: "a", UpdateTarget: &NoteRef{NoteID: 43}}, at(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	d2 := r2.Request.currentDispatch().ID
	if _, err := st.Claim(r2.Request.ID, d2, at(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Result(r2.Request.ID, d2, Result{AnswerMarkdown: "not a body"}, at(5*time.Second)); rejectReason(t, err) != RejectInvalidRequest {
		t.Fatalf("update result without proposed body should be invalid, got %v", err)
	}
}

func TestFail(t *testing.T) {
	st, _ := newTestStore(t)
	r, err := st.Create(NewRequest{Intent: IntentExplain, Instruction: "x", AgentID: "a"}, at(0))
	if err != nil {
		t.Fatal(err)
	}
	d := r.Request.currentDispatch().ID
	if _, err := st.Claim(r.Request.ID, d, at(time.Second)); err != nil {
		t.Fatal(err)
	}
	failed, err := st.Fail(r.Request.ID, d, "timeout", at(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if failed.Request.Status != StatusFailed || failed.Request.Error != "timeout" ||
		failed.Request.currentDispatch().Status != AttemptFailed {
		t.Fatalf("fail did not settle the request: %+v", failed.Request)
	}
	// Re-fail is idempotent.
	again, err := st.Fail(r.Request.ID, d, "timeout", at(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !again.Idempotent {
		t.Fatalf("re-fail should be idempotent: %+v", again)
	}
	// A queued request can be failed by the delivery boundary before an agent claims it.
	r2, err := st.Create(NewRequest{Intent: IntentExplain, Instruction: "y", AgentID: "a"}, at(4*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	failedQueued, err := st.Fail(r2.Request.ID, r2.Request.currentDispatch().ID, "delivery failed", at(5*time.Second))
	if err != nil || failedQueued.Request.Status != StatusFailed {
		t.Fatalf("fail on a queued request should settle delivery failure: %+v, %v", failedQueued.Request, err)
	}
	// Fail after completion is inactive.
	r3, err := st.Create(NewRequest{Intent: IntentExplain, Instruction: "z", AgentID: "a"}, at(6*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	d3 := r3.Request.currentDispatch().ID
	if _, err := st.Claim(r3.Request.ID, d3, at(7*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Result(r3.Request.ID, d3, Result{AnswerMarkdown: "ok"}, at(8*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Fail(r3.Request.ID, d3, "late", at(9*time.Second)); rejectReason(t, err) != RejectInactiveDispatch {
		t.Fatalf("fail after completion should be inactive, got %v", err)
	}
}

func TestCancel(t *testing.T) {
	st, _ := newTestStore(t)
	// Cancel a queued request.
	r, err := st.Create(NewRequest{Intent: IntentExplain, Instruction: "x", AgentID: "a"}, at(0))
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := st.Cancel(r.Request.ID, at(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Request.Status != StatusCancelled || cancelled.Request.currentDispatch().Status != AttemptCancelled {
		t.Fatalf("cancel did not withdraw the request: %+v", cancelled.Request)
	}
	if _, err := st.Cancel(r.Request.ID, at(2*time.Second)); err != nil {
		t.Fatal(err)
	} else if got, _ := st.Cancel(r.Request.ID, at(3*time.Second)); !got.Idempotent {
		t.Fatalf("second cancel should be idempotent")
	}
	// Later results are refused.
	if _, err := st.Result(r.Request.ID, r.Request.currentDispatch().ID, Result{AnswerMarkdown: "late"}, at(4*time.Second)); rejectReason(t, err) != RejectInactiveDispatch {
		t.Fatalf("result after cancel should be inactive, got %v", err)
	}

	// Cancel a running request.
	r2, err := st.Create(NewRequest{Intent: IntentExplain, Instruction: "y", AgentID: "a"}, at(5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	d2 := r2.Request.currentDispatch().ID
	if _, err := st.Claim(r2.Request.ID, d2, at(6*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Cancel(r2.Request.ID, at(7*time.Second)); err != nil {
		t.Fatal(err)
	}
	cancelled2, err := st.Get(r2.Request.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled2.Status != StatusCancelled || cancelled2.currentDispatch().Status != AttemptCancelled {
		t.Fatalf("running request should cancel to a cancelled attempt: %+v", cancelled2)
	}

	// Cancel a completed request is refused.
	r3, err := st.Create(NewRequest{Intent: IntentExplain, Instruction: "z", AgentID: "a"}, at(8*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	d3 := r3.Request.currentDispatch().ID
	if _, err := st.Claim(r3.Request.ID, d3, at(9*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Result(r3.Request.ID, d3, Result{AnswerMarkdown: "done"}, at(10*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Cancel(r3.Request.ID, at(11*time.Second)); rejectReason(t, err) != RejectInactiveDispatch {
		t.Fatalf("cancel after completion should be inactive, got %v", err)
	}
}

func TestRetry(t *testing.T) {
	st, _ := newTestStore(t)
	// Retry after a failure: old attempt preserved, new queued attempt, request back to queued.
	r, err := st.Create(NewRequest{Intent: IntentExplain, Instruction: "x", AgentID: "a"}, at(0))
	if err != nil {
		t.Fatal(err)
	}
	firstDispatch := r.Request.currentDispatch().ID
	if _, err := st.Claim(r.Request.ID, firstDispatch, at(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Fail(r.Request.ID, firstDispatch, "boom", at(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	retried, err := st.Retry(r.Request.ID, at(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if retried.Request.Status != StatusQueued || len(retried.Request.Attempts) != 2 {
		t.Fatalf("retry should queue a second attempt: %+v", retried.Request)
	}
	old := retried.Request.Attempts[0]
	secondDispatch := retried.Request.Attempts[1].ID
	if old.ID != firstDispatch || old.Status != AttemptFailed || secondDispatch == firstDispatch {
		t.Fatalf("retry must keep the old attempt and mint a new dispatch: %+v", retried.Request.Attempts)
	}
	if retried.Request.currentDispatch().ID != secondDispatch || retried.Request.Error != "" {
		t.Fatalf("new dispatch should be current and the error cleared: %+v", retried.Request)
	}

	// The old attempt's report is rejected as stale; the new attempt can be claimed and settled.
	if _, err := st.Result(r.Request.ID, firstDispatch, Result{AnswerMarkdown: "late"}, at(4*time.Second)); rejectReason(t, err) != RejectStaleDispatch {
		t.Fatalf("old dispatch result should be stale, got %v", err)
	}
	if _, err := st.Claim(r.Request.ID, secondDispatch, at(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Result(r.Request.ID, secondDispatch, Result{AnswerMarkdown: "fresh"}, at(6*time.Second)); err != nil {
		t.Fatal(err)
	}

	// Retry from a stalled running request supersedes the attempt.
	r2, err := st.Create(NewRequest{Intent: IntentExplain, Instruction: "y", AgentID: "a"}, at(7*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	stale := r2.Request.currentDispatch().ID
	if _, err := st.Claim(r2.Request.ID, stale, at(8*time.Second)); err != nil {
		t.Fatal(err)
	}
	retried2, err := st.Retry(r2.Request.ID, at(9*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if retried2.Request.Status != StatusQueued || retried2.Request.Attempts[0].Status != AttemptCancelled || len(retried2.Request.Attempts) != 2 {
		t.Fatalf("retry from running must close the attempt as cancelled: %+v", retried2.Request)
	}

	// Retry is refused for queued, completed, and cancelled requests.
	r3, err := st.Create(NewRequest{Intent: IntentExplain, Instruction: "z", AgentID: "a"}, at(10*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Retry(r3.Request.ID, at(11*time.Second)); rejectReason(t, err) != RejectInactiveDispatch {
		t.Fatalf("retry on a queued request should be inactive, got %v", err)
	}
	if _, err := st.Cancel(r3.Request.ID, at(12*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Retry(r3.Request.ID, at(13*time.Second)); rejectReason(t, err) != RejectInactiveDispatch {
		t.Fatalf("retry on a cancelled request should be inactive, got %v", err)
	}
	r4, err := st.Create(NewRequest{Intent: IntentExplain, Instruction: "w", AgentID: "a"}, at(14*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	d4 := r4.Request.currentDispatch().ID
	if _, err := st.Claim(r4.Request.ID, d4, at(15*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Result(r4.Request.ID, d4, Result{AnswerMarkdown: "done"}, at(16*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Retry(r4.Request.ID, at(17*time.Second)); rejectReason(t, err) != RejectInactiveDispatch {
		t.Fatalf("retry on a completed request should be inactive, got %v", err)
	}
}

func TestListOrderAndCursor(t *testing.T) {
	st, _ := newTestStore(t)
	created := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		res, err := st.Create(NewRequest{Intent: IntentExplain, Instruction: "x", AgentID: "a"}, at(time.Duration(i)*time.Second))
		if err != nil {
			t.Fatal(err)
		}
		created = append(created, res.Request.ID)
	}
	// Newest first.
	page, next, err := st.List(100, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 5 || next != "" {
		t.Fatalf("single page should hold all 5 with no cursor, got %d and %q", len(page), next)
	}
	for i, r := range page {
		if r.ID != created[len(created)-1-i] {
			t.Fatalf("page order wrong at %d: got %s want %s", i, r.ID, created[len(created)-1-i])
		}
	}
	// Cursor pagination walks the same order in two pages.
	page1, cursor, err := st.List(2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page1) != 2 || cursor == "" {
		t.Fatalf("page 1 = %d items, cursor %q", len(page1), cursor)
	}
	page2, cursor2, err := st.List(2, cursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(page2) != 2 || cursor2 == "" {
		t.Fatalf("page 2 = %d items, cursor %q", len(page2), cursor2)
	}
	page3, cursor3, err := st.List(2, cursor2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page3) != 1 || cursor3 != "" {
		t.Fatalf("page 3 = %d items, cursor %q", len(page3), cursor3)
	}
	if page2[0].ID != created[2] || page3[0].ID != created[0] {
		t.Fatal("pages do not resume where the previous one ended")
	}
	// An unknown cursor yields an empty page.
	empty, emptyCursor, err := st.List(2, "req-nope")
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 || emptyCursor != "" {
		t.Fatalf("unknown cursor should yield an empty page, got %d/%q", len(empty), emptyCursor)
	}
	// A store with no requests returns an empty, non-nil list.
	emptyStore, _ := newTestStore(t)
	none, noneCursor, err := emptyStore.List(10, "")
	if err != nil {
		t.Fatal(err)
	}
	if none == nil || len(none) != 0 || noneCursor != "" {
		t.Fatalf("empty store should return [], got %v/%q", none, noneCursor)
	}
}

func TestGetAndLoadRejectUnreadableFiles(t *testing.T) {
	st, _ := newTestStore(t)
	if _, err := st.Get("nope"); rejectReason(t, err) != RejectUnknownRequest {
		t.Fatalf("missing request should be unknown_request, got %v", err)
	}
	// A request file with a bad version is refused at load, not half-read.
	dir := filepath.Join(st.dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "req-bad.json"), []byte(`{"version":99,"id":"req-bad","status":"queued"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Get("req-bad"); err == nil || !strings.Contains(err.Error(), "unsupported version") {
		t.Fatalf("bad version should be refused, got %v", err)
	}
}

func TestListSkipsTempAndForeignFiles(t *testing.T) {
	st, cfg := newTestStore(t)
	if _, err := st.Create(NewRequest{Intent: IntentExplain, Instruction: "x", AgentID: "a"}, at(0)); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(cfg.TrackDir(), RequestsDirName)
	// A leftover hidden temp file and a non-request file must not surface in the listing.
	if err := os.WriteFile(filepath.Join(dir, ".req-1.json.tmp-abc"), []byte(`garbage`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scratch.txt"), []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	reqs, next, err := st.List(10, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 1 || next != "" {
		t.Fatalf("listing must skip temp and foreign files, got %d items", len(reqs))
	}
}

func TestPersistenceAcrossTransitions(t *testing.T) {
	st, cfg := newTestStore(t)
	r, err := st.Create(NewRequest{Intent: IntentResearch, Instruction: "research", AgentID: "a"}, at(0))
	if err != nil {
		t.Fatal(err)
	}
	d := r.Request.currentDispatch().ID
	if _, err := st.Claim(r.Request.ID, d, at(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Result(r.Request.ID, d, Result{AnswerMarkdown: "answer", Sources: []string{"src1"}}, at(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	// A fresh store reads the fully-settled request from disk.
	reopened, err := New(cfg).Get(r.Request.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Status != StatusCompleted || reopened.Result.AnswerMarkdown != "answer" ||
		len(reopened.Result.Sources) != 1 || reopened.currentDispatch().Status != AttemptCompleted {
		t.Fatalf("settled request did not survive a reload: %+v", reopened)
	}
}

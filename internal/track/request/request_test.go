package request

import (
	"encoding/json"
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

// TestUpdateResultEntersApplying verifies the stage-5 entry: an update result records the proposed
// body and enters applying (not completed), with the attempt still running because the server-side
// apply is in flight. The apply transitions (CompleteApply/Conflict/FailApply) are exercised in
// apply_test.go.
func TestUpdateResultEntersApplying(t *testing.T) {
	st, cfg := newTestStore(t)
	target := &NoteRef{NoteID: 42, ETag: "abc123", Body: "# Old body\n"}
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
	settled, err := st.Result(r.Request.ID, d, Result{ProposedBody: "# New body\n", AnswerMarkdown: "The intro is now clearer."}, at(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if settled.Request.Status != StatusApplying || settled.Request.Result == nil {
		t.Fatalf("update result must enter applying: %+v", settled.Request)
	}
	if settled.Request.Result.ProposedBody != "# New body\n" || settled.Request.Result.Fingerprint == "" {
		t.Fatalf("proposal not recorded: %+v", settled.Request.Result)
	}
	if d := settled.Request.currentDispatch(); d.Status != AttemptRunning || d.ResultFingerprint == "" {
		t.Fatalf("attempt must stay running until the apply settles: %+v", d)
	}
	// The applying state is durable: a fresh store reads it back.
	reopened, err := New(cfg).Get(r.Request.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Status != StatusApplying {
		t.Fatalf("applying state did not persist: %+v", reopened)
	}

	// The identical result re-submitted while applying is an idempotent replay; a different one is refused.
	replay, err := st.Result(r.Request.ID, d, Result{ProposedBody: "# New body\n", AnswerMarkdown: "The intro is now clearer."}, at(3*time.Second))
	if err != nil || !replay.Idempotent {
		t.Fatalf("same result replay while applying should be idempotent: %+v, %v", replay, err)
	}
	if _, err := st.Result(r.Request.ID, d, Result{ProposedBody: "# Different\n"}, at(4*time.Second)); rejectReason(t, err) != RejectResultConflict {
		t.Fatalf("different result while applying should be result_conflict, got %v", err)
	}
	// An update result without a proposed body is invalid.
	r2, err := st.Create(NewRequest{Intent: IntentUpdate, Instruction: "x", AgentID: "a", UpdateTarget: &NoteRef{NoteID: 43}}, at(5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	d2 := r2.Request.currentDispatch().ID
	if _, err := st.Claim(r2.Request.ID, d2, at(6*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Result(r2.Request.ID, d2, Result{AnswerMarkdown: "not a body"}, at(7*time.Second)); rejectReason(t, err) != RejectInvalidRequest {
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

// completeRequest drives a request through create -> claim -> result and returns the settled request.
func completeRequest(t *testing.T, st *Store, in NewRequest, answer string, now time.Time) Request {
	t.Helper()
	res, err := st.Create(in, now)
	if err != nil {
		t.Fatal(err)
	}
	d := res.Request.currentDispatch().ID
	if _, err := st.Claim(res.Request.ID, d, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Result(res.Request.ID, d, Result{AnswerMarkdown: answer}, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	got, err := st.Get(res.Request.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestSaveRecordsTheAnswerNote(t *testing.T) {
	st, cfg := newTestStore(t)
	r := completeRequest(t, st, NewRequest{Intent: IntentExplain, Instruction: "explain", AgentID: "a"}, "# The answer\n", at(0))

	saved, err := st.Save(r.ID, SaveInput{ClientRequestID: "save-1", Title: "The answer", NoteID: 42}, at(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if saved.Reused {
		t.Fatal("first save must not be a reuse")
	}
	sv := saved.Request.Result.Saved
	if sv == nil || sv.NoteID != 42 || sv.Title != "The answer" || sv.Status != SaveStatusSaved ||
		sv.ClientRequestID != "save-1" || sv.SavedAt == "" || sv.Vault != "" {
		t.Fatalf("saved note not recorded: %+v", saved.Request.Result)
	}
	// The save is a record on the result, never a move of the request or its attempt.
	if saved.Request.Status != StatusCompleted || saved.Request.currentDispatch().Status != AttemptCompleted {
		t.Fatalf("save must not move the request: %+v", saved.Request)
	}
	// The answer and the fingerprint survive the save untouched.
	if saved.Request.Result.AnswerMarkdown != "# The answer\n" || saved.Request.Result.Fingerprint == "" {
		t.Fatalf("save must not touch the result: %+v", saved.Request.Result)
	}

	// Re-saving the same title+vault is an idempotent replay returning the recorded note.
	again, err := st.Save(r.ID, SaveInput{ClientRequestID: "save-1", Title: "The answer", NoteID: 42}, at(4*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !again.Reused || again.Request.Result.Saved.NoteID != 42 {
		t.Fatalf("re-save should be idempotent: %+v", again)
	}
	// A replay without the key reuses too — the request's own record is the key that matters.
	keyless, err := st.Save(r.ID, SaveInput{Title: "The answer", NoteID: 42}, at(5*time.Second))
	if err != nil || !keyless.Reused {
		t.Fatalf("keyless re-save should reuse the recorded note: %+v, %v", keyless, err)
	}

	// A different title — or the same title aimed at a different vault — is refused, never recorded
	// over the old note.
	if _, err := st.Save(r.ID, SaveInput{Title: "Rewritten", NoteID: 43}, at(6*time.Second)); rejectReason(t, err) != RejectAlreadySaved {
		t.Fatalf("different title should be already_saved, got %v", err)
	}
	if _, err := st.Save(r.ID, SaveInput{Title: "The answer", Vault: "work", NoteID: 44}, at(7*time.Second)); rejectReason(t, err) != RejectAlreadySaved {
		t.Fatalf("different vault should be already_saved, got %v", err)
	}

	// The save survives a restart: a fresh store over the same vault reads it back.
	reopened, err := New(cfg).Get(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Result.Saved == nil || reopened.Result.Saved.NoteID != 42 ||
		reopened.Result.Saved.Title != "The answer" || reopened.Result.Saved.Status != SaveStatusSaved {
		t.Fatalf("save did not persist across a reload: %+v", reopened.Result)
	}
}

func TestSaveGuards(t *testing.T) {
	st, _ := newTestStore(t)

	// Unknown request.
	if _, err := st.Save("nope", SaveInput{Title: "x", NoteID: 1}, at(0)); rejectReason(t, err) != RejectUnknownRequest {
		t.Fatalf("unknown request should be unknown_request, got %v", err)
	}

	// Input guards on a completed explain request: blank title, oversize title, missing note id.
	r := completeRequest(t, st, NewRequest{Intent: IntentExplain, Instruction: "x", AgentID: "a"}, "ok", at(time.Second))
	if _, err := st.Save(r.ID, SaveInput{Title: "  ", NoteID: 1}, at(3*time.Second)); rejectReason(t, err) != RejectInvalidRequest {
		t.Fatalf("blank title should be invalid_request, got %v", err)
	}
	if _, err := st.Save(r.ID, SaveInput{Title: strings.Repeat("t", MaxSaveTitleBytes+1), NoteID: 1}, at(3*time.Second)); rejectReason(t, err) != RejectOversize {
		t.Fatalf("oversize title should be oversize, got %v", err)
	}
	if _, err := st.Save(r.ID, SaveInput{Title: "x"}, at(3*time.Second)); rejectReason(t, err) != RejectInvalidRequest {
		t.Fatalf("missing note id should be invalid_request, got %v", err)
	}

	// A request that is not completed cannot be saved.
	queued, err := st.Create(NewRequest{Intent: IntentExplain, Instruction: "q", AgentID: "a"}, at(4*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Save(queued.Request.ID, SaveInput{Title: "x", NoteID: 1}, at(5*time.Second)); rejectReason(t, err) != RejectNotSaveable {
		t.Fatalf("queued request should be not_saveable, got %v", err)
	}
	running, err := st.Create(NewRequest{Intent: IntentExplain, Instruction: "run", AgentID: "a"}, at(6*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	rd := running.Request.currentDispatch().ID
	if _, err := st.Claim(running.Request.ID, rd, at(7*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Save(running.Request.ID, SaveInput{Title: "x", NoteID: 1}, at(8*time.Second)); rejectReason(t, err) != RejectNotSaveable {
		t.Fatalf("running request should be not_saveable, got %v", err)
	}
	failed, err := st.Create(NewRequest{Intent: IntentExplain, Instruction: "f", AgentID: "a"}, at(9*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	fd := failed.Request.currentDispatch().ID
	if _, err := st.Claim(failed.Request.ID, fd, at(10*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Fail(failed.Request.ID, fd, "boom", at(11*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Save(failed.Request.ID, SaveInput{Title: "x", NoteID: 1}, at(12*time.Second)); rejectReason(t, err) != RejectNotSaveable {
		t.Fatalf("failed request should be not_saveable, got %v", err)
	}
	cancelled, err := st.Create(NewRequest{Intent: IntentExplain, Instruction: "c", AgentID: "a"}, at(13*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Cancel(cancelled.Request.ID, at(14*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Save(cancelled.Request.ID, SaveInput{Title: "x", NoteID: 1}, at(15*time.Second)); rejectReason(t, err) != RejectNotSaveable {
		t.Fatalf("cancelled request should be not_saveable, got %v", err)
	}

	// An update request completes with a proposed body, but saving is for explain/research answers:
	// update apply is a later stage and must not be short-circuited into a new note.
	upd, err := st.Create(NewRequest{Intent: IntentUpdate, Instruction: "u", AgentID: "a", UpdateTarget: &NoteRef{NoteID: 9}}, at(16*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	ud := upd.Request.currentDispatch().ID
	if _, err := st.Claim(upd.Request.ID, ud, at(17*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Result(upd.Request.ID, ud, Result{ProposedBody: "# New body\n"}, at(18*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Save(upd.Request.ID, SaveInput{Title: "x", NoteID: 1}, at(19*time.Second)); rejectReason(t, err) != RejectNotSaveable {
		t.Fatalf("update request should be not_saveable, got %v", err)
	}

	// The save client_request_id is a vault-wide key: a second request reusing the key is refused,
	// and the original request's record is untouched.
	first := completeRequest(t, st, NewRequest{Intent: IntentResearch, Instruction: "r1", AgentID: "a"}, "a1", at(19*time.Second))
	if _, err := st.Save(first.ID, SaveInput{ClientRequestID: "shared-key", Title: "First", NoteID: 1}, at(20*time.Second)); err != nil {
		t.Fatal(err)
	}
	second := completeRequest(t, st, NewRequest{Intent: IntentResearch, Instruction: "r2", AgentID: "a"}, "a2", at(21*time.Second))
	if _, err := st.Save(second.ID, SaveInput{ClientRequestID: "shared-key", Title: "Second", NoteID: 2}, at(22*time.Second)); rejectReason(t, err) != RejectInputConflict {
		t.Fatalf("save key reuse across requests should be input_conflict, got %v", err)
	}
	got, err := st.Get(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Result.Saved == nil || got.Result.Saved.NoteID != 1 || got.Result.Saved.Title != "First" {
		t.Fatalf("key reuse must not touch the original save: %+v", got.Result.Saved)
	}
}

func TestFollowUpSeedsParentContext(t *testing.T) {
	st, cfg := newTestStore(t)
	parent := completeRequest(t, st, NewRequest{
		Intent:      IntentResearch,
		Instruction: "Is X the bottleneck?",
		AgentID:     "a",
		Context:     Context{Quote: "two-phase commit"},
	}, "# X is not the bottleneck\n", at(0))

	child, err := st.Create(NewRequest{
		ClientRequestID: "child-1",
		ParentRequestID: parent.ID,
		Intent:          IntentResearch,
		Instruction:     "What about Y?",
		AgentID:         "a",
	}, at(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if child.Request.ParentRequestID != parent.ID {
		t.Fatalf("child must record its parent: %+v", child.Request)
	}
	pa := child.Request.Context.PriorAnswers
	if len(pa) != 1 {
		t.Fatalf("child must carry one prior answer, got %d: %+v", len(pa), child.Request.Context)
	}
	if pa[0].RequestID != parent.ID || pa[0].Instruction != "Is X the bottleneck?" || pa[0].AnswerMarkdown != "# X is not the bottleneck\n" {
		t.Fatalf("prior answer wrong: %+v", pa[0])
	}
	// The child keeps its own context; the prior chain is additive, not a replacement.
	if child.Request.Context.Quote != "" {
		t.Fatalf("child context must stay the client's: %+v", child.Request.Context)
	}

	// The parent is never mutated by the follow-up.
	reloaded, err := st.Get(parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Context.PriorAnswers) != 0 || reloaded.Context.Quote != "two-phase commit" ||
		reloaded.Result.AnswerMarkdown != "# X is not the bottleneck\n" {
		t.Fatalf("parent must not be mutated by the follow-up: %+v", reloaded)
	}

	// A grandchild carries the whole chain: the parent's turn then the child's.
	grandchild, err := st.Create(NewRequest{
		ParentRequestID: child.Request.ID,
		Intent:          IntentResearch,
		Instruction:     "And Z?",
		AgentID:         "a",
	}, at(4*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	chain := grandchild.Request.Context.PriorAnswers
	if len(chain) != 2 || chain[0].RequestID != parent.ID || chain[1].RequestID != child.Request.ID {
		t.Fatalf("grandchild must carry the full chain: %+v", chain)
	}

	// The follow-up persists across a restart, and an idempotent replay reuses the child.
	reopened, err := New(cfg).Get(child.Request.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.ParentRequestID != parent.ID || len(reopened.Context.PriorAnswers) != 1 {
		t.Fatalf("follow-up did not persist: %+v", reopened)
	}
	again, err := st.Create(NewRequest{
		ClientRequestID: "child-1",
		ParentRequestID: parent.ID,
		Intent:          IntentResearch,
		Instruction:     "What about Y?",
		AgentID:         "a",
	}, at(5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !again.Reused || again.Request.ID != child.Request.ID {
		t.Fatalf("follow-up replay should reuse the child: %+v", again)
	}

	// An unknown parent is refused before anything is stored.
	if _, err := st.Create(NewRequest{ParentRequestID: "req-nope", Intent: IntentExplain, Instruction: "x", AgentID: "a"}, at(6*time.Second)); rejectReason(t, err) != RejectInvalidRequest {
		t.Fatalf("unknown parent should be invalid_request, got %v", err)
	}
	if got := len(listIDs(t, st)); got != 3 {
		t.Fatalf("refused follow-up must store nothing, got %d requests", got)
	}
}

func TestSetDeliveryRecordsOutcomeOnAttempt(t *testing.T) {
	st, _ := newTestStore(t)
	r, err := st.Create(NewRequest{Intent: IntentExplain, Instruction: "x", AgentID: "a"}, at(0))
	if err != nil {
		t.Fatal(err)
	}
	d := r.Request.currentDispatch().ID

	// The connection records its own outcome; the request stays queued — a send is not a start.
	got, err := st.SetDelivery(r.Request.ID, d, DeliverySent, "sent", at(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if got.Idempotent || got.Request.Status != StatusQueued {
		t.Fatalf("delivery must not move the request: %+v", got.Request)
	}
	attempt := got.Request.currentDispatch()
	if attempt.Delivery != DeliverySent || attempt.DeliveryNote != "sent" {
		t.Fatalf("delivery not recorded on the attempt: %+v", attempt)
	}
	// The same outcome re-recorded is an idempotent success.
	again, err := st.SetDelivery(r.Request.ID, d, DeliverySent, "sent", at(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !again.Idempotent {
		t.Fatalf("identical delivery record should be idempotent")
	}
	// A later, different outcome overwrites the record (unknown -> sent re-attempt flow).
	unknown, err := st.SetDelivery(r.Request.ID, d, DeliveryUnknown, "send timed out; delivery outcome unknown", at(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if unknown.Idempotent || unknown.Request.currentDispatch().Delivery != DeliveryUnknown {
		t.Fatalf("delivery outcome should be replaceable: %+v", unknown.Request.currentDispatch())
	}

	// Invalid status, unknown request, unknown dispatch, and missing dispatch id are refused.
	if _, err := st.SetDelivery(r.Request.ID, d, DeliveryStatus("exploded"), "", at(0)); rejectReason(t, err) != RejectInvalidRequest {
		t.Fatalf("bad delivery status should be invalid_request, got %v", err)
	}
	if _, err := st.SetDelivery("nope", d, DeliverySent, "", at(0)); rejectReason(t, err) != RejectUnknownRequest {
		t.Fatalf("unknown request should be unknown_request, got %v", err)
	}
	if _, err := st.SetDelivery(r.Request.ID, "d-1", DeliverySent, "", at(0)); rejectReason(t, err) != RejectUnknownDispatch {
		t.Fatalf("unknown dispatch should be unknown_dispatch, got %v", err)
	}
	if _, err := st.SetDelivery(r.Request.ID, "", DeliverySent, "", at(0)); rejectReason(t, err) != RejectInvalidRequest {
		t.Fatalf("missing dispatch id should be invalid_request, got %v", err)
	}
}

func TestSetDeliverySkipsSupersededAttempt(t *testing.T) {
	st, _ := newTestStore(t)
	r, err := st.Create(NewRequest{Intent: IntentExplain, Instruction: "x", AgentID: "a"}, at(0))
	if err != nil {
		t.Fatal(err)
	}
	first := r.Request.currentDispatch().ID
	if _, err := st.Fail(r.Request.ID, first, "agent died", at(time.Second)); err != nil {
		t.Fatal(err)
	}
	retried, err := st.Retry(r.Request.ID, at(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	second := retried.Request.currentDispatch().ID

	// A late delivery report for the superseded attempt is skipped, not written anywhere.
	got, err := st.SetDelivery(r.Request.ID, first, DeliverySent, "sent", at(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Idempotent {
		t.Fatalf("stale delivery should be an idempotent skip")
	}
	if got.Request.currentDispatch().Delivery != "" {
		t.Fatalf("stale delivery touched the live attempt: %+v", got.Request.currentDispatch())
	}
	// The live attempt still records its own delivery normally.
	if _, err := st.SetDelivery(r.Request.ID, second, DeliverySent, "sent", at(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	if got.Request.currentDispatch().ID != second {
		t.Fatalf("unexpected current attempt")
	}
}

// A saved answer can cross from a named request vault into the launch vault.
// Clients inherit enclosing vault labels, so omitting the empty destination
// would route the saved note back into the request's vault.
func TestSavedNoteJSONKeepsLaunchVault(t *testing.T) {
	raw, err := json.Marshal(Request{Vault: "other", Result: &Result{Saved: &SavedNote{Vault: "", NoteID: 42}}})
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		Result struct {
			Saved map[string]any `json:"saved"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	if vault, exists := response.Result.Saved["vault"]; !exists || vault != "" {
		t.Fatalf("saved launch vault must be explicit, got %s", raw)
	}
}

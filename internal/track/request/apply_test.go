package request

import (
	"strings"
	"testing"
	"time"
)

// applyingRequest drives an update request into the applying state: create (with a send-time
// target), claim, and result. It returns the stored request; the attempt stays running because the
// server-side apply is what settles it.
func applyingRequest(t *testing.T, st *Store, now time.Time) Request {
	t.Helper()
	created, err := st.Create(NewRequest{
		Intent:       IntentUpdate,
		Instruction:  "Make it clearer.",
		AgentID:      "a",
		UpdateTarget: &NoteRef{NoteID: 42, ETag: "etag-1", Body: "# Old body\n"},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	d := created.Request.currentDispatch().ID
	if _, err := st.Claim(created.Request.ID, d, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	settled, err := st.Result(created.Request.ID, d, Result{ProposedBody: "# New body\n", AnswerMarkdown: "Clearer now."}, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if settled.Request.Status != StatusApplying {
		t.Fatalf("helper must leave the request applying: %+v", settled.Request)
	}
	return settled.Request
}

func TestCompleteApplyRecordsBeforeAndAfter(t *testing.T) {
	st, cfg := newTestStore(t)
	now := at(0)
	r := applyingRequest(t, st, now)

	settled, err := st.CompleteApply(r.ID, ApplyInput{
		BeforeBody: "# Old body\n",
		BeforeETag: "etag-1",
		AfterBody:  "# New body\n",
		AfterETag:  "etag-2",
	}, now.Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if settled.Idempotent || settled.Request.Status != StatusCompleted {
		t.Fatalf("CompleteApply must settle to completed: %+v", settled.Request)
	}
	d := settled.Request.currentDispatch()
	if d.Status != AttemptCompleted || d.SettledAt == "" {
		t.Fatalf("attempt must be completed: %+v", d)
	}
	ap := settled.Request.Result.Apply
	if ap == nil || ap.BeforeBody != "# Old body\n" || ap.BeforeETag != "etag-1" ||
		ap.AfterBody != "# New body\n" || ap.AfterETag != "etag-2" || ap.AppliedAt == "" {
		t.Fatalf("apply record wrong: %+v", ap)
	}
	// The change rationale rides as the apply record's reason.
	if ap.Reason != "Clearer now." {
		t.Fatalf("apply reason must be the agent's change rationale, got %q", ap.Reason)
	}
	if settled.Request.Error != "" {
		t.Fatalf("a completed apply must not carry an error: %q", settled.Request.Error)
	}

	// The record survives a reload, so recovery can trust it.
	reopened, err := New(cfg).Get(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Status != StatusCompleted || reopened.Result.Apply.AfterBody != "# New body\n" {
		t.Fatalf("apply record did not persist: %+v", reopened)
	}

	// Replaying the identical apply is an idempotent success.
	again, err := st.CompleteApply(r.ID, ApplyInput{
		BeforeBody: "# Old body\n", BeforeETag: "etag-1",
		AfterBody: "# New body\n", AfterETag: "etag-2",
	}, now.Add(4*time.Second))
	if err != nil || !again.Idempotent {
		t.Fatalf("repeated apply should be idempotent: %+v, %v", again, err)
	}
}

func TestCompleteApplyGuards(t *testing.T) {
	st, _ := newTestStore(t)
	now := at(0)
	r := applyingRequest(t, st, now)

	// Once completed, a different outcome cannot be recorded over the settled apply.
	if _, err := st.CompleteApply(r.ID, ApplyInput{AfterBody: "# New body\n", AfterETag: "etag-2"}, now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CompleteApply(r.ID, ApplyInput{AfterBody: "# Different\n", AfterETag: "etag-9"}, now.Add(4*time.Second)); rejectReason(t, err) != RejectInactiveDispatch {
		t.Fatalf("re-apply with a different outcome should be inactive, got %v", err)
	}

	// A request that is not applying cannot be completed.
	created, err := st.Create(NewRequest{Intent: IntentExplain, Instruction: "x", AgentID: "a"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CompleteApply(created.Request.ID, ApplyInput{}, now); rejectReason(t, err) != RejectInactiveDispatch {
		t.Fatalf("complete on a queued request should be inactive, got %v", err)
	}
	// Unknown request.
	if _, err := st.CompleteApply("nope", ApplyInput{}, now); rejectReason(t, err) != RejectUnknownRequest {
		t.Fatalf("unknown request should be unknown_request, got %v", err)
	}
}

func TestConflictPreservesAnswerProposalAndReason(t *testing.T) {
	st, cfg := newTestStore(t)
	now := at(0)
	r := applyingRequest(t, st, now)

	settled, err := st.Conflict(r.ID, "update target changed since the request was sent", now.Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if settled.Request.Status != StatusConflict || settled.Request.Error != "update target changed since the request was sent" {
		t.Fatalf("conflict not recorded: %+v", settled.Request)
	}
	// The answer and the proposal are preserved for the panel's diff and for a retry.
	if settled.Request.Result.ProposedBody != "# New body\n" || settled.Request.Result.AnswerMarkdown != "Clearer now." {
		t.Fatalf("conflict must keep the proposal and answer: %+v", settled.Request.Result)
	}
	// The attempt closes as failed, so the retry affordance appears alongside failures.
	if d := settled.Request.currentDispatch(); d.Status != AttemptFailed || d.FailureReason == "" {
		t.Fatalf("conflicted attempt must be failed: %+v", d)
	}
	if ap := settled.Request.Result.Apply; ap == nil || ap.Reason != "update target changed since the request was sent" || ap.AppliedAt == "" {
		t.Fatalf("conflict reason not recorded on the apply record: %+v", ap)
	}

	// The conflict persists across a reload.
	reopened, err := New(cfg).Get(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Status != StatusConflict || reopened.Result.ProposedBody != "# New body\n" {
		t.Fatalf("conflict did not persist: %+v", reopened)
	}

	// The same conflict reason re-recorded is an idempotent success; a different one is refused.
	again, err := st.Conflict(r.ID, "update target changed since the request was sent", now.Add(4*time.Second))
	if err != nil || !again.Idempotent {
		t.Fatalf("repeated conflict should be idempotent: %+v, %v", again, err)
	}
	if _, err := st.Conflict(r.ID, "something else", now.Add(5*time.Second)); rejectReason(t, err) != RejectInactiveDispatch {
		t.Fatalf("different conflict over a conflicted request should be inactive, got %v", err)
	}
}

func TestConflictGuards(t *testing.T) {
	st, _ := newTestStore(t)
	now := at(0)

	// Only applying update requests can be conflicted.
	created, err := st.Create(NewRequest{Intent: IntentUpdate, Instruction: "x", AgentID: "a", UpdateTarget: &NoteRef{NoteID: 1}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Conflict(created.Request.ID, "nope", now); rejectReason(t, err) != RejectInactiveDispatch {
		t.Fatalf("conflict on a queued request should be inactive, got %v", err)
	}
	if _, err := st.Conflict("nope", "nope", now); rejectReason(t, err) != RejectUnknownRequest {
		t.Fatalf("unknown request should be unknown_request, got %v", err)
	}
}

func TestFailApplySettlesProcessingFailure(t *testing.T) {
	st, _ := newTestStore(t)
	now := at(0)
	r := applyingRequest(t, st, now)

	settled, err := st.FailApply(r.ID, "replace note: disk full", now.Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if settled.Request.Status != StatusFailed || settled.Request.Error != "replace note: disk full" {
		t.Fatalf("apply failure not recorded: %+v", settled.Request)
	}
	if d := settled.Request.currentDispatch(); d.Status != AttemptFailed || d.FailureReason != "replace note: disk full" {
		t.Fatalf("failed apply attempt wrong: %+v", d)
	}
	// The proposal is kept so the user can retry.
	if settled.Request.Result.ProposedBody != "# New body\n" {
		t.Fatalf("failed apply must keep the proposal: %+v", settled.Request.Result)
	}
	// Re-failing the same apply is idempotent; an oversize reason is refused.
	again, err := st.FailApply(r.ID, "replace note: disk full", now.Add(4*time.Second))
	if err != nil || !again.Idempotent {
		t.Fatalf("re-fail should be idempotent: %+v, %v", again, err)
	}
	if _, err := st.FailApply(r.ID, strings.Repeat("x", MaxFailureReasonBytes+1), now); rejectReason(t, err) != RejectOversize {
		t.Fatalf("oversize apply failure should be oversize, got %v", err)
	}

	// Retry from a failed apply mints a new attempt.
	retried, err := st.Retry(r.ID, now.Add(5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if retried.Request.Status != StatusQueued || len(retried.Request.Attempts) != 2 {
		t.Fatalf("retry after failed apply must queue a new attempt: %+v", retried.Request)
	}
}

func TestCancelRefusedWhileApplying(t *testing.T) {
	st, _ := newTestStore(t)
	now := at(0)
	r := applyingRequest(t, st, now)

	// The spec: once a request is applying, cancel is refused and the apply settles it.
	if _, err := st.Cancel(r.ID, now.Add(3*time.Second)); rejectReason(t, err) != RejectInactiveDispatch {
		t.Fatalf("cancel while applying should be inactive, got %v", err)
	}
	// The applying state is untouched.
	got, err := st.Get(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusApplying {
		t.Fatalf("refused cancel must not move the request: %+v", got)
	}
}

func TestSameResultReplayAfterApplyCompletion(t *testing.T) {
	st, _ := newTestStore(t)
	now := at(0)
	r := applyingRequest(t, st, now)
	d := r.currentDispatch().ID
	if _, err := st.CompleteApply(r.ID, ApplyInput{AfterBody: "# New body\n", AfterETag: "etag-2"}, now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	// The identical result re-submitted after completion is an idempotent success.
	replay, err := st.Result(r.ID, d, Result{ProposedBody: "# New body\n", AnswerMarkdown: "Clearer now."}, now.Add(4*time.Second))
	if err != nil || !replay.Idempotent || replay.Request.Status != StatusCompleted {
		t.Fatalf("same result replay after apply should be idempotent: %+v, %v", replay, err)
	}
	// A different result cannot overwrite the completed state.
	if _, err := st.Result(r.ID, d, Result{ProposedBody: "# Rewritten\n"}, now.Add(5*time.Second)); rejectReason(t, err) != RejectResultConflict {
		t.Fatalf("different result over completed should be result_conflict, got %v", err)
	}
}

func TestRetryAfterConflictMintsFreshDispatch(t *testing.T) {
	st, _ := newTestStore(t)
	now := at(0)
	r := applyingRequest(t, st, now)
	first := r.currentDispatch().ID
	if _, err := st.Conflict(r.ID, "target moved", now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	retried, err := st.Retry(r.ID, now.Add(4*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if retried.Request.Status != StatusQueued || len(retried.Request.Attempts) != 2 {
		t.Fatalf("retry after conflict must queue a new attempt: %+v", retried.Request)
	}
	second := retried.Request.currentDispatch().ID
	if second == first {
		t.Fatal("retry must mint a new dispatch id")
	}
	// The old dispatch's result cannot settle the new attempt.
	if _, err := st.Result(r.ID, first, Result{ProposedBody: "stale"}, now.Add(5*time.Second)); rejectReason(t, err) != RejectStaleDispatch {
		t.Fatalf("old dispatch result after retry should be stale, got %v", err)
	}
	// A fresh result on the new attempt enters applying again.
	if _, err := st.Claim(r.ID, second, now.Add(6*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Result(r.ID, second, Result{ProposedBody: "# Fresh\n"}, now.Add(7*time.Second)); err != nil {
		t.Fatal(err)
	}
	got, err := st.Get(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusApplying || got.Result.ProposedBody != "# Fresh\n" {
		t.Fatalf("retried update should re-enter applying: %+v", got)
	}
}

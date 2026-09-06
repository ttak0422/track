package note

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/dispatch"
)

func settleCfg(t *testing.T) *config.Config {
	t.Helper()
	cfg := execCfg(t)
	// A note exists at this id so unknown_task vs unknown_dispatch are distinguishable.
	path := cfg.NotePath(1000)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# Note\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfg
}

// seed executes a dispatch: it appends a pending then a dispatched record, so the note holds one
// live dispatch owned by the given id.
func seed(t *testing.T, cfg *config.Config, id int64, dispatchID string) {
	t.Helper()
	now := time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)
	if _, err := AppendExecTransition(cfg, id, dispatch.Record{
		ID: dispatchID, Note: id, Status: dispatch.StatusPending,
	}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := AppendExecTransition(cfg, id, dispatch.Record{
		ID: dispatchID, Note: id, Status: dispatch.StatusDispatched,
		DispatchedAt: now.Format(time.RFC3339),
	}, now); err != nil {
		t.Fatal(err)
	}
}

func settleTime() time.Time {
	return time.Date(2026, 9, 6, 10, 30, 0, 0, time.UTC)
}

func TestSettleDispatchCompletesLiveAttempt(t *testing.T) {
	cfg := settleCfg(t)
	seed(t, cfg, 1000, "d-1000-1")
	now := settleTime()

	res, err := SettleDispatch(cfg, 1000, dispatch.Record{
		ID: "d-1000-1", Note: 1000, Status: dispatch.StatusCompleted,
		CompletedAt: now.Format(time.RFC3339),
	}, dispatch.StatusDispatched, now)
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	if !res.Settled {
		t.Fatalf("expected settlement, got reason %q", res.Reason)
	}
	if res.Record.Status != dispatch.StatusCompleted || res.Record.Note != 1000 {
		t.Fatalf("unexpected settled record: %+v", res.Record)
	}

	meta, _, err := ReadMetadata(cfg.MetadataPath(1000))
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.ExecLog) != 3 {
		t.Fatalf("exec log has %d records, want 3", len(meta.ExecLog))
	}
	if meta.ExecLog[2].Status != dispatch.StatusCompleted {
		t.Fatalf("completion not appended: %+v", meta.ExecLog[2])
	}
}

func TestSettleDispatchHeartbeatRefreshes(t *testing.T) {
	cfg := settleCfg(t)
	seed(t, cfg, 1000, "d-1000-1")
	now := settleTime()

	res, err := SettleDispatch(cfg, 1000, dispatch.Record{
		ID:              "d-1000-1",
		Note:            1000,
		Status:          dispatch.StatusDispatched,
		LastHeartbeatAt: now.Format(time.RFC3339),
	}, dispatch.StatusDispatched, now)
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	if !res.Settled {
		t.Fatalf("expected heartbeat settlement, got reason %q", res.Reason)
	}

	meta, _, _ := ReadMetadata(cfg.MetadataPath(1000))
	last := meta.ExecLog[len(meta.ExecLog)-1]
	if last.Status != dispatch.StatusDispatched || last.LastHeartbeatAt != now.Format(time.RFC3339) {
		t.Fatalf("heartbeat not refreshed: %+v", last)
	}
}

func TestSettleDispatchRejectsUnknownTask(t *testing.T) {
	cfg := settleCfg(t)
	seed(t, cfg, 1000, "d-1000-1")
	// Remove the note file so no note carries that id (orphan sidecar remains).
	if err := os.Remove(cfg.NotePath(1000)); err != nil {
		t.Fatal(err)
	}
	res, err := SettleDispatch(cfg, 1000, dispatch.Record{
		ID: "d-1000-1", Note: 1000, Status: dispatch.StatusCompleted,
	}, dispatch.StatusDispatched, settleTime())
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	if res.Settled || res.Reason != dispatch.RejectUnknownTask {
		t.Fatalf("expected unknown_task, got settled=%v reason=%q", res.Settled, res.Reason)
	}
}

func TestSettleDispatchRejectsUnknownDispatch(t *testing.T) {
	cfg := settleCfg(t)
	seed(t, cfg, 1000, "d-1000-1")
	res, err := SettleDispatch(cfg, 1000, dispatch.Record{
		ID: "d-1000-999", Note: 1000, Status: dispatch.StatusCompleted,
	}, dispatch.StatusDispatched, settleTime())
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	if res.Settled || res.Reason != dispatch.RejectUnknownDispatch {
		t.Fatalf("expected unknown_dispatch, got settled=%v reason=%q", res.Settled, res.Reason)
	}
}

func TestSettleDispatchRejectsTaskDispatchMismatch(t *testing.T) {
	cfg := settleCfg(t)
	seed(t, cfg, 1000, "d-1000-1")
	// A report that resolves to a record, but one that belongs to a different note, is a mismatch.
	// AppendExecTransition refuses a record whose Note differs from the sidecar it lands in, so the
	// mismatch record is written straight to the sidecar to make the branch reachable — as a hand
	// edited or imported log could hold.
	meta, _, err := ReadMetadata(cfg.MetadataPath(1000))
	if err != nil {
		t.Fatal(err)
	}
	meta.ExecLog = append(meta.ExecLog, dispatch.Record{
		ID: "d-2000-1", Note: 2000, Status: dispatch.StatusPending,
	})
	if err := WriteMetadata(cfg.MetadataPath(1000), meta); err != nil {
		t.Fatal(err)
	}
	res, err := SettleDispatch(cfg, 1000, dispatch.Record{
		ID: "d-2000-1", Note: 1000, Status: dispatch.StatusCompleted,
	}, dispatch.StatusPending, settleTime())
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	if res.Settled || res.Reason != dispatch.RejectTaskDispatchMismatch {
		t.Fatalf("expected task_dispatch_mismatch, got settled=%v reason=%q", res.Settled, res.Reason)
	}
}

func TestSettleDispatchRejectsStaleDispatch(t *testing.T) {
	cfg := settleCfg(t)
	seed(t, cfg, 1000, "d-1000-1") // first attempt owns the note
	seed(t, cfg, 1000, "d-1000-2") // retry: second attempt is now current
	now := settleTime()

	// A late completion from the failed first attempt must not settle the live second attempt.
	res, err := SettleDispatch(cfg, 1000, dispatch.Record{
		ID: "d-1000-1", Note: 1000, Status: dispatch.StatusCompleted,
		CompletedAt: now.Format(time.RFC3339),
	}, dispatch.StatusDispatched, now)
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	if res.Settled || res.Reason != dispatch.RejectStaleDispatch {
		t.Fatalf("expected stale_dispatch, got settled=%v reason=%q", res.Settled, res.Reason)
	}
	// The live second attempt is untouched.
	meta, _, _ := ReadMetadata(cfg.MetadataPath(1000))
	if meta.ExecLog[len(meta.ExecLog)-1].ID != "d-1000-2" {
		t.Fatalf("stale report mutated the live dispatch: %+v", meta.ExecLog[len(meta.ExecLog)-1])
	}
}

func TestSettleDispatchRejectsInactiveDispatch(t *testing.T) {
	cfg := settleCfg(t)
	seed(t, cfg, 1000, "d-1000-1")
	now := settleTime()

	// Settle the dispatch once.
	if res, err := SettleDispatch(cfg, 1000, dispatch.Record{
		ID: "d-1000-1", Note: 1000, Status: dispatch.StatusCompleted,
		CompletedAt: now.Format(time.RFC3339),
	}, dispatch.StatusDispatched, now); err != nil || !res.Settled {
		t.Fatalf("first settle: settled=%v err=%v", res.Settled, err)
	}
	// A duplicate completion with the same expected pre-transition status is now inactive: the
	// stored status is completed, not dispatched, so the compare-and-swap fails.
	res, err := SettleDispatch(cfg, 1000, dispatch.Record{
		ID: "d-1000-1", Note: 1000, Status: dispatch.StatusCompleted,
		CompletedAt: now.Format(time.RFC3339),
	}, dispatch.StatusDispatched, now)
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	if res.Settled || res.Reason != dispatch.RejectInactiveDispatch {
		t.Fatalf("expected inactive_dispatch, got settled=%v reason=%q", res.Settled, res.Reason)
	}
	// A completion against a dispatch that moved from pending to dispatched mid-report is also
	// inactive: the expected pending no longer matches the stored dispatched.
	cfg2 := settleCfg(t)
	seed(t, cfg2, 1000, "d-1000-1")
	res, err = SettleDispatch(cfg2, 1000, dispatch.Record{
		ID: "d-1000-1", Note: 1000, Status: dispatch.StatusCompleted,
	}, dispatch.StatusPending, now)
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	if res.Settled || res.Reason != dispatch.RejectInactiveDispatch {
		t.Fatalf("expected inactive_dispatch on CAS mismatch, got settled=%v reason=%q", res.Settled, res.Reason)
	}
}

func TestSettleDispatchRejectsMissingDispatchID(t *testing.T) {
	cfg := settleCfg(t)
	_, err := SettleDispatch(cfg, 1000, dispatch.Record{
		Note: 1000, Status: dispatch.StatusCompleted,
	}, dispatch.StatusDispatched, settleTime())
	if err == nil {
		t.Fatal("expected a missing-dispatch-id error")
	}
}

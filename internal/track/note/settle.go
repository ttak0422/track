package note

import (
	"fmt"
	"os"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/dispatch"
)

// SettleResult is the outcome of one settlement attempt: whether the report applied, the rejection
// reason when it did not, and the appended record when it did.
type SettleResult struct {
	Settled bool
	Reason  dispatch.RejectReason
	Record  dispatch.Record
}

// SettleDispatch resolves a completion or heartbeat report against a note's execution log and, when
// it names the live attempt, applies it under a compare-and-swap write.
//
// A report is keyed by BOTH the note id and the dispatch id — the dual-key invariant
// (docs/spec/agent-state-model.md): the note id names *what*, the pair names *which attempt*. id is
// the note the report claims to settle; rec carries the reported dispatch id (rec.ID), the reported
// note (rec.Note), and the target status (rec.Status: `completed`/`failed` for a completion, or
// `dispatched` for a heartbeat whose last_heartbeat_at it refreshes). The reported dispatch id must
// name the dispatch that is currently active for that note; a report keyed by either key alone has
// no settlement path here.
//
// expected is the pre-transition status the reporter believed the dispatch was in — the `--expect`
// assertion of agent-workflows.md lifted to execution records. The transition is appended only when
// the stored status still equals expected (the compare-and-swap); a dispatch whose status changed
// while a report was in flight is rejected, never overwritten.
//
// The rejection reasons are the stale-completion set:
//
//	unknown_task            — no note with that id.
//	unknown_dispatch        — no execution record with that id.
//	task_dispatch_mismatch  — the execution record belongs to a different note.
//	inactive_dispatch       — the dispatch is already settled, or its state moved while the report
//	                          was in flight (the compare-and-swap failed).
//	stale_dispatch          — the dispatch is not the current dispatch for the note.
//
// The transition is appended to the note's sidecar exec_log (authoritative storage), exactly like a
// task-log append; it is never derived from the note body or written to the index.
func SettleDispatch(cfg *config.Config, id int64, rec dispatch.Record, expected dispatch.Status, now time.Time) (SettleResult, error) {
	if rec.ID == "" {
		return SettleResult{}, fmt.Errorf("settlement report has no dispatch id")
	}
	// unknown_task: no note with that id. A note with only an orphan sidecar (its file gone) reads
	// as unknown too, matching "no note with that id".
	if _, err := os.Stat(cfg.NotePath(id)); err != nil {
		if os.IsNotExist(err) {
			return SettleResult{Reason: dispatch.RejectUnknownTask}, nil
		}
		return SettleResult{}, fmt.Errorf("stat note %d: %w", id, err)
	}

	meta, found, err := ReadMetadata(cfg.MetadataPath(id))
	if err != nil {
		return SettleResult{}, fmt.Errorf("read metadata: %w", err)
	}
	// A note with no sidecar cannot hold an execution record for this dispatch.
	if !found {
		return SettleResult{Reason: dispatch.RejectUnknownDispatch}, nil
	}

	// Resolve the reported dispatch to its latest record in this note's log.
	var targetIdx = -1
	for i := range meta.ExecLog {
		if meta.ExecLog[i].ID == rec.ID {
			targetIdx = i
		}
	}
	if targetIdx == -1 {
		return SettleResult{Reason: dispatch.RejectUnknownDispatch}, nil
	}
	// task_dispatch_mismatch: the record belongs to a different note.
	if meta.ExecLog[targetIdx].Note != id {
		return SettleResult{Reason: dispatch.RejectTaskDispatchMismatch}, nil
	}

	// stale_dispatch: the reported dispatch is not the note's current (latest) dispatch. The log is
	// append-only, so the last record names the live attempt; a report from a superseded attempt is
	// rejected here, before the compare-and-swap.
	cur := latestRecord(meta.ExecLog)
	if cur == nil || cur.ID != rec.ID {
		return SettleResult{Reason: dispatch.RejectStaleDispatch}, nil
	}

	// Compare-and-swap: apply only when the stored status still equals the expected pre-transition
	// status. A dispatch that already settled, or whose status moved while the report was in flight,
	// is inactive — rejected, not overwritten.
	if meta.ExecLog[targetIdx].Status != expected {
		return SettleResult{Reason: dispatch.RejectInactiveDispatch}, nil
	}

	rec.Note = id
	rec.At = "" // stamped by AppendExecTransition
	appended, err := AppendExecTransition(cfg, id, rec, now)
	if err != nil {
		return SettleResult{}, fmt.Errorf("append settlement: %w", err)
	}
	return SettleResult{Settled: true, Record: appended}, nil
}

// latestRecord returns the last record of an append-only execution log, which is the note's current
// dispatch state. It is nil for an empty log.
func latestRecord(log []dispatch.Record) *dispatch.Record {
	if len(log) == 0 {
		return nil
	}
	return &log[len(log)-1]
}

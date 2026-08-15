package note

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/task"
)

// ErrStateMismatch reports that the task line was not in the state the caller asserted, so nothing
// was written. It is the task-level counterpart of the note save's etag conflict.
var ErrStateMismatch = errors.New("task state mismatch")

// ApplyTaskState is the single write path for task state changes, shared by the CLI (task set, the
// legacy toggle) and the web workspace's board. It rewrites the task line in the note file (stamping
// or clearing the [done:date] token and recomputing progress cookies) and, when the state actually
// changed, appends the transition to the note's sidecar task log — so history survives without
// polluting the body. Callers reindex the note afterwards, matching the other mutation commands.
//
// expect, when non-empty, is the state the caller believes the line is in: a mismatch refuses the
// write with ErrStateMismatch instead of flipping whatever is there now. Every caller reads the
// line, decides a target, and only then calls this — so without the assertion each carries its own
// window in which someone else's edit lands in between.
// ponytail: asserting the state catches a concurrent state change, not a concurrent line shift —
// an inserted line above makes `line` a different task, and if that task is in the expected state
// too the guard passes. Assert the task text if that ever bites.
func ApplyTaskState(cfg *config.Config, notePath string, line int, state, expect string, now time.Time) (task.Transition, error) {
	raw, err := os.ReadFile(notePath)
	if err != nil {
		return task.Transition{}, fmt.Errorf("read note: %w", err)
	}
	updated, tr, err := task.SetState(string(raw), line, state, now)
	if err != nil {
		return task.Transition{}, err
	}
	if expect != "" {
		if _, ok := task.StateNamed(expect); !ok {
			return task.Transition{}, fmt.Errorf("unknown state %q", expect)
		}
		if !strings.EqualFold(expect, tr.From) {
			return task.Transition{}, fmt.Errorf("%w: line %d is %s, not %s", ErrStateMismatch, line, tr.From, expect)
		}
	}
	// Cookies may need a rewrite even when the state itself did not change (a stale cookie).
	if updated != string(raw) {
		if err := os.WriteFile(notePath, []byte(updated), 0o644); err != nil {
			return task.Transition{}, fmt.Errorf("write note: %w", err)
		}
	}
	if !tr.Changed {
		return tr, nil
	}
	if err := appendTaskTransition(cfg, notePath, tr, now); err != nil {
		return task.Transition{}, err
	}
	return tr, nil
}

// TaskPatch is one optimistic task mutation. ETag identifies the exact note snapshot whose line
// coordinates the client displayed; state and date changes are all derived from that one read and
// committed with one note write.
type TaskPatch struct {
	Line   int
	State  string
	Expect string
	Sched  *string
	Due    *string
	ETag   string
}

// ApplyTaskPatch applies a task request from one verified note snapshot. Keeping the etag check and
// every line-based transformation on the same raw bytes closes the check-then-reopen window that
// would otherwise let a same-state insertion redirect the mutation to another task.
func ApplyTaskPatch(cfg *config.Config, notePath string, patch TaskPatch, now time.Time) (task.Transition, error) {
	raw, err := os.ReadFile(notePath)
	if err != nil {
		return task.Transition{}, fmt.Errorf("read note: %w", err)
	}
	if patch.ETag != "" {
		if err := CheckContentETag(raw, patch.ETag); err != nil {
			return task.Transition{}, err
		}
	}

	updated := string(raw)
	var tr task.Transition
	if patch.State != "" {
		updated, tr, err = task.SetState(updated, patch.Line, patch.State, now)
		if err != nil {
			return task.Transition{}, err
		}
		if patch.Expect != "" {
			if _, ok := task.StateNamed(patch.Expect); !ok {
				return task.Transition{}, fmt.Errorf("unknown state %q", patch.Expect)
			}
			if !strings.EqualFold(patch.Expect, tr.From) {
				return task.Transition{}, fmt.Errorf("%w: line %d is %s, not %s", ErrStateMismatch, patch.Line, tr.From, patch.Expect)
			}
		}
	} else if patch.Expect != "" {
		if _, ok := task.StateNamed(patch.Expect); !ok {
			return task.Transition{}, fmt.Errorf("unknown state %q", patch.Expect)
		}
		cur, ok := task.At(updated, patch.Line)
		if !ok {
			return task.Transition{}, fmt.Errorf("line %d is not a task checkbox", patch.Line)
		}
		if !strings.EqualFold(patch.Expect, cur.State) {
			return task.Transition{}, fmt.Errorf("%w: line %d is %s, not %s", ErrStateMismatch, patch.Line, cur.State, patch.Expect)
		}
	}

	for _, datePatch := range []struct {
		field task.DateField
		value *string
	}{{task.SchedField, patch.Sched}, {task.DueField, patch.Due}} {
		if datePatch.value == nil {
			continue
		}
		updated, _, err = task.SetDate(updated, patch.Line, datePatch.field, *datePatch.value)
		if err != nil {
			return task.Transition{}, err
		}
	}
	if updated != string(raw) {
		if err := os.WriteFile(notePath, []byte(updated), 0o644); err != nil {
			return task.Transition{}, fmt.Errorf("write note: %w", err)
		}
	}
	if tr.Changed {
		if err := appendTaskTransition(cfg, notePath, tr, now); err != nil {
			return task.Transition{}, err
		}
	}
	return tr, nil
}

func appendTaskTransition(cfg *config.Config, notePath string, tr task.Transition, now time.Time) error {

	id, err := IDFromPath(notePath)
	if err != nil {
		return fmt.Errorf("invalid note path: %w", err)
	}
	metaPath := cfg.MetadataPath(id)
	meta, found, err := ReadMetadata(metaPath)
	if err != nil {
		return fmt.Errorf("read metadata: %w", err)
	}
	if !found {
		meta = Metadata{Created: now.Format(cfg.DateFormat)}
	}
	meta.TaskLog = append(meta.TaskLog, task.LogEntry{
		At:   now.Format("2006-01-02 15:04:05"),
		Line: tr.Line,
		From: tr.From,
		To:   tr.To,
		Text: tr.Text,
	})
	if err := WriteMetadata(metaPath, meta); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}
	return nil
}

// ApplyTaskAppend adds a new open task line at the end of a note file: the CLI counterpart of
// task.Append, sharing the same read-write path as the other task mutations. No sidecar log entry —
// the task log records state transitions, and a new task starts in the first open state. The caller
// reindexes afterwards, which stamps the note's activity day like any other mutation.
func ApplyTaskAppend(notePath string, o task.AppendOpts) (task.Task, error) {
	raw, err := os.ReadFile(notePath)
	if err != nil {
		return task.Task{}, fmt.Errorf("read note: %w", err)
	}
	updated, t, err := task.Append(string(raw), o)
	if err != nil {
		return task.Task{}, err
	}
	if err := os.WriteFile(notePath, []byte(updated), 0o644); err != nil {
		return task.Task{}, fmt.Errorf("write note: %w", err)
	}
	return t, nil
}

// ApplyTaskDate writes a task's scheduled or due date in a note file, the date counterpart of
// ApplyTaskState: same addressing (path plus 1-based line), same in-place rewrite. No sidecar log
// entry — the task log records state transitions, and a date is not one. Callers reindex afterwards.
//
// expect carries the same assertion ApplyTaskState takes, for the same reason: a date is picked
// against a task the caller has already read, so without it a re-date lands on whatever the line
// became in between. The state is what is asserted — a date write does not change it, so the check
// is purely "is this still the task I was looking at".
// ponytail: asserting the state catches a concurrent state change, not a concurrent line shift —
// an inserted line above makes `line` a different task, and if that task is in the expected state
// too the guard passes. Assert the task text if that ever bites.
func ApplyTaskDate(notePath string, line int, field task.DateField, date, expect string) (task.Task, error) {
	raw, err := os.ReadFile(notePath)
	if err != nil {
		return task.Task{}, fmt.Errorf("read note: %w", err)
	}
	if expect != "" {
		if _, ok := task.StateNamed(expect); !ok {
			return task.Task{}, fmt.Errorf("unknown state %q", expect)
		}
		cur, ok := task.At(string(raw), line)
		if !ok {
			return task.Task{}, fmt.Errorf("line %d is not a task checkbox", line)
		}
		if !strings.EqualFold(expect, cur.State) {
			return task.Task{}, fmt.Errorf("%w: line %d is %s, not %s", ErrStateMismatch, line, cur.State, expect)
		}
	}
	updated, t, err := task.SetDate(string(raw), line, field, date)
	if err != nil {
		return task.Task{}, err
	}
	if updated != string(raw) {
		if err := os.WriteFile(notePath, []byte(updated), 0o644); err != nil {
			return task.Task{}, fmt.Errorf("write note: %w", err)
		}
	}
	return t, nil
}

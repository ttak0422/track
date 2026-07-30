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

	id, err := IDFromPath(notePath)
	if err != nil {
		return task.Transition{}, fmt.Errorf("invalid note path: %w", err)
	}
	metaPath := cfg.MetadataPath(id)
	meta, found, err := ReadMetadata(metaPath)
	if err != nil {
		return task.Transition{}, fmt.Errorf("read metadata: %w", err)
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
		return task.Transition{}, fmt.Errorf("write metadata: %w", err)
	}
	return tr, nil
}

// ApplyTaskDate writes a task's scheduled or due date in a note file, the date counterpart of
// ApplyTaskState: same addressing (path plus 1-based line), same in-place rewrite. No sidecar log
// entry — the task log records state transitions, and a date is not one. Callers reindex afterwards.
func ApplyTaskDate(notePath string, line int, field, date string) (task.Task, error) {
	raw, err := os.ReadFile(notePath)
	if err != nil {
		return task.Task{}, fmt.Errorf("read note: %w", err)
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

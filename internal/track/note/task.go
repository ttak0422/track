package note

import (
	"fmt"
	"os"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/task"
)

// ApplyTaskState is the single write path for task state changes, shared by the CLI (task set, the
// legacy toggle) and the web workspace's board. It rewrites the task line in the note file (stamping
// or clearing the [done:date] token and recomputing progress cookies) and, when the state actually
// changed, appends the transition to the note's sidecar task log — so history survives without
// polluting the body. Callers reindex the note afterwards, matching the other mutation commands.
func ApplyTaskState(cfg *config.Config, notePath string, line int, state string, now time.Time) (task.Transition, error) {
	raw, err := os.ReadFile(notePath)
	if err != nil {
		return task.Transition{}, fmt.Errorf("read note: %w", err)
	}
	updated, tr, err := task.SetState(string(raw), line, state, cfg.TaskStates, now)
	if err != nil {
		return task.Transition{}, err
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

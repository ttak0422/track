package store

import (
	"strings"

	"github.com/ttak0422/track/internal/track/task"
)

// TaskFilter selects and orders rows for the tasks listing. Zero values mean "no filter". DueBy and
// OverdueBefore are YYYY-MM-DD dates: DueBy keeps not-done tasks whose deadline is on or before the
// date ("what is due by Friday"); OverdueBefore keeps not-done tasks whose deadline already passed.
type TaskFilter struct {
	NoteID        int64
	States        []string
	DueBy         string
	OverdueBefore string
	ByPriority    bool
}

// TaskRow is one task in the cross-note listing: the parsed task plus its note's identity.
type TaskRow struct {
	NoteID   int64  `json:"note_id"`
	FileKind string `json:"file_kind"`
	Title    string `json:"title"`
	Path     string `json:"path,omitempty"`
	task.Task
}

// Tasks lists indexed tasks matching the filter. The default order is by note then line; ByPriority
// puts open tasks first, then [#A] before [#B] before unprioritized, breaking ties by deadline.
func (s *Store) Tasks(f TaskFilter) ([]TaskRow, error) {
	query := `SELECT t.note_id, n.kind, n.title, t.line, t.state, t.done, t.priority, t.scheduled, t.due, t.completed, t.text
	 FROM tasks t JOIN notes n ON n.id = t.note_id`
	var conds []string
	var args []any
	if f.NoteID != 0 {
		conds = append(conds, "t.note_id = ?")
		args = append(args, f.NoteID)
	}
	if len(f.States) > 0 {
		conds = append(conds, "t.state COLLATE NOCASE IN (?"+strings.Repeat(", ?", len(f.States)-1)+")")
		for _, st := range f.States {
			args = append(args, st)
		}
	}
	if f.DueBy != "" {
		conds = append(conds, "t.due <> '' AND t.due <= ? AND t.done = 0")
		args = append(args, f.DueBy)
	}
	if f.OverdueBefore != "" {
		conds = append(conds, "t.due <> '' AND t.due < ? AND t.done = 0")
		args = append(args, f.OverdueBefore)
	}
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	if f.ByPriority {
		query += ` ORDER BY t.done, (t.priority = ''), t.priority, (t.due = ''), t.due, t.note_id, t.line`
	} else {
		query += ` ORDER BY t.note_id, t.line`
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TaskRow
	for rows.Next() {
		var r TaskRow
		if err := rows.Scan(&r.NoteID, &r.FileKind, &r.Title, &r.Line, &r.State, &r.Done,
			&r.Priority, &r.Scheduled, &r.Due, &r.Completed, &r.Text); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

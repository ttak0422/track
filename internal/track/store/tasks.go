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
	// Priorities keeps only tasks whose priority token ([#A]) is one of the listed letters,
	// case-insensitively. Tasks without a priority token never match.
	Priorities []string
	// TextContains keeps only tasks whose human text contains the substring, case-insensitively.
	// The match is literal: LIKE wildcards in the needle are not treated as wildcards.
	TextContains string
	// Day keeps only not-done tasks scheduled on or due on this exact YYYY-MM-DD day — the
	// day's agenda. Tasks due that day come first, then scheduled-only ones.
	Day string
	// Dated keeps only tasks carrying a scheduled or due date — the ones that belong on a calendar.
	Dated bool
	// Open keeps only tasks in a state whose terminal flag is false, whatever that state is named.
	// It says nothing about dates; a "still to do" listing that wants only committed work ANDs it
	// with Dated.
	Open       bool
	ByPriority bool
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
// puts open tasks first, then [#A] before [#B] before unprioritized, breaking ties by deadline. A
// Day filter puts tasks due that day before tasks merely scheduled for it, both by note then line.
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
	if len(f.Priorities) > 0 {
		conds = append(conds, "t.priority <> '' AND t.priority COLLATE NOCASE IN (?"+strings.Repeat(", ?", len(f.Priorities)-1)+")")
		for _, p := range f.Priorities {
			args = append(args, p)
		}
	}
	if f.TextContains != "" {
		// instr over lower() is a literal substring test: the needle's LIKE wildcards stay literal,
		// which --text is meant to mean.
		conds = append(conds, "instr(lower(t.text), lower(?)) > 0")
		args = append(args, f.TextContains)
	}
	if f.DueBy != "" {
		conds = append(conds, "t.due <> '' AND t.due <= ? AND t.done = 0")
		args = append(args, f.DueBy)
	}
	if f.OverdueBefore != "" {
		conds = append(conds, "t.due <> '' AND t.due < ? AND t.done = 0")
		args = append(args, f.OverdueBefore)
	}
	if f.Day != "" {
		conds = append(conds, "t.done = 0 AND (t.scheduled = ? OR t.due = ?)")
		args = append(args, f.Day, f.Day)
	}
	if f.Dated {
		conds = append(conds, "(t.scheduled <> '' OR t.due <> '')")
	}
	if f.Open {
		conds = append(conds, "t.done = 0")
	}
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	switch {
	case f.Day != "":
		// Due that day first: a deadline reads as more urgent than a plan for the day.
		query += ` ORDER BY (t.due = ''), t.note_id, t.line`
	case f.ByPriority:
		query += ` ORDER BY t.done, (t.priority = ''), t.priority, (t.due = ''), t.due, t.note_id, t.line`
	default:
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

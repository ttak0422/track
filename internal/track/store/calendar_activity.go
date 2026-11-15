package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const CalendarActivityRowLimit = 16384
const CalendarActivityLabelBytes = 256

var ErrCalendarActivityUnavailable = errors.New("calendar activity index is unavailable or changed")

type CalendarTaskPreview struct {
	NoteID        int64  `json:"note_id"`
	FileKind      string `json:"file_kind"`
	Line          int    `json:"line"`
	Text          string `json:"text"`
	TextTruncated bool   `json:"text_truncated"`
	State         string `json:"state"`
	Done          bool   `json:"done"`
	Priority      string `json:"priority"`
	Scheduled     string `json:"scheduled"`
	Due           string `json:"due"`
}
type CalendarNotePreview struct {
	NoteID         int64  `json:"note_id"`
	FileKind       string `json:"file_kind"`
	Title          string `json:"title"`
	TitleTruncated bool   `json:"title_truncated"`
}
type CalendarActivityDay struct {
	Date      string                `json:"date"`
	TaskCount int                   `json:"task_count"`
	Tasks     []CalendarTaskPreview `json:"tasks"`
	NoteCount int                   `json:"note_count"`
	Notes     []CalendarNotePreview `json:"notes"`
}

func calendarLabel(prefix []byte, size int) (string, bool, error) {
	cut := size > CalendarActivityLabelBytes
	if !cut && size != len(prefix) {
		return "", false, errors.New("invalid calendar label")
	}
	if cut {
		// SQL returns a byte prefix; discard only an incomplete trailing UTF-8 rune.
		for len(prefix) > 0 && !utf8.Valid(prefix) && len(prefix) > 256 {
			prefix = prefix[:len(prefix)-1]
		}
	}
	if !utf8.Valid(prefix) {
		return "", false, errors.New("invalid calendar label")
	}
	label := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, string(prefix))
	if !cut && len(label) <= CalendarActivityLabelBytes {
		return label, false, nil
	}
	end := len(label)
	if end > CalendarActivityLabelBytes-len("…") {
		end = CalendarActivityLabelBytes - len("…")
	}
	for end > 0 && end < len(label) && !utf8.RuneStart(label[end]) {
		end--
	}
	return label[:end] + "…", true, nil
}

func calendarIndexHealth(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}) (int64, error) {
	var initialized bool
	var revision int64
	var raw string
	err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM index_state WHERE key='link_format'),
 COALESCE((SELECT CAST(value AS INTEGER) FROM index_state WHERE key='snapshot_revision'),0),
 COALESCE((SELECT substr(value,1,1048577) FROM index_state WHERE key='read_state'),'')`).Scan(&initialized, &revision, &raw)
	if err != nil {
		return 0, err
	}
	var health ReadState
	if !initialized || len(raw) > 1<<20 || json.Unmarshal([]byte(raw), &health) != nil || health.Partial || health.CacheUsed || health.CheckedAt <= 0 {
		return 0, ErrCalendarActivityUnavailable
	}
	return revision, nil
}

// CalendarActivityRevisionContext verifies persisted health and revision without refreshing the index.
func (s *Store) CalendarActivityRevisionContext(ctx context.Context, revision int64) error {
	current, err := calendarIndexHealth(ctx, s.db)
	if err != nil {
		return err
	}
	if current != revision {
		return ErrCalendarActivityUnavailable
	}
	return nil
}

// CalendarActivityRangeContext reads one verified index snapshot. Result rows are capped;
// the context also bounds scans of large histories (scheduled dates have no dedicated index).
func (s *Store) CalendarActivityRangeContext(ctx context.Context, since, until string) ([]CalendarActivityDay, int64, error) {
	first, e1 := time.Parse(time.DateOnly, since)
	last, e2 := time.Parse(time.DateOnly, until)
	if e1 != nil || e2 != nil || first.Year() < 1 || last.Year() > 9998 || since != first.Format(time.DateOnly) || until != last.Format(time.DateOnly) || last.Before(first) || !last.Before(first.AddDate(0, 0, 42)) {
		return nil, 0, errors.New("invalid calendar activity range")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	revision, err := calendarIndexHealth(ctx, tx)
	if err != nil {
		return nil, 0, err
	}
	days := []CalendarActivityDay{}
	positions := map[string]int{}
	for day := first; !day.After(last); day = day.AddDate(0, 0, 1) {
		date := day.Format(time.DateOnly)
		positions[date] = len(days)
		days = append(days, CalendarActivityDay{Date: date, Tasks: []CalendarTaskPreview{}, Notes: []CalendarNotePreview{}})
	}
	rows, err := tx.QueryContext(ctx, `SELECT t.note_id,substr(n.kind,1,32),t.line,substr(CAST(t.text AS BLOB),1,260),length(CAST(t.text AS BLOB)),
 substr(t.state,1,32),t.done,substr(t.priority,1,8),substr(t.scheduled,1,32),substr(t.due,1,32)
 FROM tasks t JOIN notes n ON n.id=t.note_id WHERE (t.scheduled BETWEEN ? AND ?) OR (t.due BETWEEN ? AND ?) LIMIT ?`, since, until, since, until, CalendarActivityRowLimit+1)
	if err != nil {
		return nil, 0, err
	}
	tasks := []CalendarTaskPreview{}
	for rows.Next() {
		var t CalendarTaskPreview
		var raw []byte
		var size int
		if err = rows.Scan(&t.NoteID, &t.FileKind, &t.Line, &raw, &size, &t.State, &t.Done, &t.Priority, &t.Scheduled, &t.Due); err != nil {
			break
		}
		if len(tasks) == CalendarActivityRowLimit {
			err = fmt.Errorf("%w: matching task row budget exceeded", ErrCalendarActivityUnavailable)
			break
		}
		t.Text, t.TextTruncated, err = calendarLabel(raw, size)
		if err != nil {
			break
		}
		if t.NoteID <= 0 || t.Line <= 0 || (t.FileKind != "note" && t.FileKind != "journal") {
			err = errors.New("invalid indexed task identity")
			break
		}
		validState := t.State == "TODO" || t.State == "DOING" || t.State == "WAITING" || t.State == "DONE" || t.State == "CANCELLED"
		if !validState || t.Done != (t.State == "DONE" || t.State == "CANCELLED") || (t.Priority != "" && (len(t.Priority) != 1 || t.Priority[0] < 'A' || t.Priority[0] > 'Z')) || len(t.Scheduled) > 10 || len(t.Due) > 10 {
			err = errors.New("invalid indexed task fields")
			break
		}
		tasks = append(tasks, t)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].NoteID != tasks[j].NoteID {
			return tasks[i].NoteID < tasks[j].NoteID
		}
		return tasks[i].Line < tasks[j].Line
	})
	for _, t := range tasks {
		for i, date := range []string{t.Scheduled, t.Due} {
			if i == 1 && date == t.Scheduled {
				continue
			}
			if pos, ok := positions[date]; ok {
				d := &days[pos]
				d.TaskCount++
				if len(d.Tasks) < 3 {
					d.Tasks = append(d.Tasks, t)
				}
			}
		}
	}
	rows, err = tx.QueryContext(ctx, `SELECT d.day,n.id,substr(n.kind,1,32),substr(CAST(n.title AS BLOB),1,260),length(CAST(n.title AS BLOB)),n.mtime
 FROM note_days d JOIN notes n ON n.id=d.note_id WHERE d.day BETWEEN ? AND ? LIMIT ?`, since, until, CalendarActivityRowLimit+1)
	if err != nil {
		return nil, 0, err
	}
	type datedNote struct {
		date    string
		preview CalendarNotePreview
		mtime   int64
	}
	notes := []datedNote{}
	for rows.Next() {
		var n datedNote
		var raw []byte
		var size int
		if err = rows.Scan(&n.date, &n.preview.NoteID, &n.preview.FileKind, &raw, &size, &n.mtime); err != nil {
			break
		}
		if len(notes) == CalendarActivityRowLimit {
			err = fmt.Errorf("%w: matching note activity row budget exceeded", ErrCalendarActivityUnavailable)
			break
		}
		n.preview.Title, n.preview.TitleTruncated, err = calendarLabel(raw, size)
		if err != nil {
			break
		}
		if n.preview.NoteID <= 0 || n.preview.FileKind != "note" {
			err = errors.New("invalid indexed note activity identity")
			break
		}
		if _, ok := positions[n.date]; !ok {
			err = errors.New("invalid indexed activity date")
			break
		}
		notes = append(notes, n)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	sort.Slice(notes, func(i, j int) bool {
		if notes[i].mtime != notes[j].mtime {
			return notes[i].mtime > notes[j].mtime
		}
		return notes[i].preview.NoteID < notes[j].preview.NoteID
	})
	for _, n := range notes {
		d := &days[positions[n.date]]
		d.NoteCount++
		if len(d.Notes) < 3 {
			d.Notes = append(d.Notes, n.preview)
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, 0, err
	}
	if err = s.CalendarActivityRevisionContext(ctx, revision); err != nil {
		return nil, 0, err
	}
	return days, revision, nil
}

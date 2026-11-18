package store

import (
	"context"
	"errors"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/task"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"
)

func readyCalendarStore(t *testing.T, s *Store) {
	t.Helper()
	if err := s.SetIndexedLinkFormat(""); err != nil {
		t.Fatal(err)
	}
	if err := s.SetReadState(ReadState{CheckedAt: time.Now().UnixMilli()}); err != nil {
		t.Fatal(err)
	}
}
func TestCalendarActivityMembershipOrdering(t *testing.T) {
	s := newTestStore(t)
	readyCalendarStore(t, s)
	for i := int64(4); i >= 1; i-- {
		n := &note.Note{ID: i, Mtime: i / 2, Meta: note.Metadata{Title: "N", Days: []string{"2024-02-29", "2024-03-01"}}, Tasks: []task.Task{{Line: 3, State: "DONE", Done: true, Text: "done", Scheduled: "2024-02-29", Due: "2024-02-29"}, {Line: 2, State: "TODO", Text: "next", Due: "2024-03-01"}}}
		if err := s.UpsertNote(n); err != nil {
			t.Fatal(err)
		}
		if i == 2 {
			got, _, err := s.CalendarActivityRangeContext(context.Background(), "2024-02-29", "2024-02-29")
			if err != nil || got[0].NoteCount != 3 || got[0].TaskCount != 3 || len(got[0].Notes) != 3 || len(got[0].Tasks) != 3 {
				t.Fatal(got, err)
			}
		}
	}
	if err := s.UpsertNote(&note.Note{ID: 9, Kind: "journal", Meta: note.Metadata{Title: "Journal", Days: []string{"2024-02-29"}}}); err != nil {
		t.Fatal(err)
	}
	got, rev, err := s.CalendarActivityRangeContext(context.Background(), "2024-02-28", "2024-03-01")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].TaskCount != 0 || got[0].NoteCount != 0 || got[0].Tasks == nil || got[0].Notes == nil {
		t.Fatal(got)
	}
	for _, d := range got[1:] {
		if d.TaskCount != 4 || d.NoteCount != 4 || len(d.Tasks) != 3 || len(d.Notes) != 3 {
			t.Fatal(d)
		}
		for i, p := range d.Tasks {
			if p.NoteID != int64(i+1) {
				t.Fatal(d)
			}
		}
		if d.Notes[0].NoteID != 4 || d.Notes[1].NoteID != 2 || d.Notes[2].NoteID != 3 {
			t.Fatal(d)
		}
	}
	if !got[1].Tasks[0].Done || got[1].Tasks[0].State != "DONE" {
		t.Fatal(got[1])
	}
	if err := s.CalendarActivityRevisionContext(context.Background(), rev); err != nil {
		t.Fatal(err)
	}
	if err := s.InvalidateSnapshot(); err != nil {
		t.Fatal(err)
	}
	if err := s.CalendarActivityRevisionContext(context.Background(), rev); !errors.Is(err, ErrCalendarActivityUnavailable) {
		t.Fatal(err)
	}
}
func TestCalendarActivityLabels(t *testing.T) {
	for _, text := range []string{strings.Repeat("a", 255), strings.Repeat("a", 256), strings.Repeat("a", 257), strings.Repeat("界", 100), strings.Repeat("🙂", 100), strings.Repeat("<", 1000), "a\nb\tc", strings.Repeat("\u0085", 200)} {
		raw := []byte(text)
		if len(raw) > 260 {
			raw = raw[:260]
		}
		label, cut, err := calendarLabel(raw, len(text))
		if err != nil || !utf8.ValidString(label) || len(label) > 256 || cut != (len(text) > 256) {
			t.Fatal(len(label), cut, err)
		}
		if !cut && label != strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return ' '
			}
			return r
		}, text) {
			t.Fatal("unnecessarily changed label")
		}
	}
	if _, _, err := calendarLabel([]byte{255}, 1); err == nil {
		t.Fatal("invalid UTF8 accepted")
	}
}
func TestCalendarActivityReadinessAndCancellation(t *testing.T) {
	s := newTestStore(t)
	if _, _, err := s.CalendarActivityRangeContext(context.Background(), "2024-02-29", "2024-02-29"); !errors.Is(err, ErrCalendarActivityUnavailable) {
		t.Fatal(err)
	}
	readyCalendarStore(t, s)
	for _, state := range []ReadState{{CheckedAt: 1, Partial: true}, {CheckedAt: 1, CacheUsed: true}, {}} {
		if err := s.SetReadState(state); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.CalendarActivityRangeContext(context.Background(), "2024-02-29", "2024-02-29"); !errors.Is(err, ErrCalendarActivityUnavailable) {
			t.Fatal(err)
		}
	}
	readyCalendarStore(t, s)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := s.CalendarActivityRangeContext(ctx, "2024-02-29", "2024-02-29"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{"2024-02-01", "2024-03-14"}, {"2023-02-29", "2023-03-01"}, {"0000-01-01", "0001-01-01"}, {"2024-03-01", "2024-02-29"}} {
		if _, _, err := s.CalendarActivityRangeContext(context.Background(), pair[0], pair[1]); err == nil {
			t.Fatal(pair)
		}
	}
	for _, pair := range [][2]string{{"2024-02-01", "2024-03-13"}, {"2024-12-31", "2025-01-01"}, {"0001-01-01", "0001-01-01"}, {"9998-12-31", "9998-12-31"}} {
		if _, _, err := s.CalendarActivityRangeContext(context.Background(), pair[0], pair[1]); err != nil {
			t.Fatal(pair, err)
		}
	}
}
func TestCalendarActivityMatchingRowCaps(t *testing.T) {
	for _, kind := range []string{"tasks", "notes"} {
		t.Run(kind, func(t *testing.T) {
			s := newTestStore(t)
			readyCalendarStore(t, s)
			tx, err := s.db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			for i := 1; i <= CalendarActivityRowLimit; i++ {
				if _, err = tx.Exec(`INSERT INTO notes(id,kind,title) VALUES(?,'note','N')`, i); err != nil {
					t.Fatal(err)
				}
				if kind == "tasks" {
					_, err = tx.Exec(`INSERT INTO tasks(note_id,line,state,scheduled) VALUES(?,1,'TODO','2024-02-29')`, i)
				} else {
					_, err = tx.Exec(`INSERT INTO note_days(note_id,day) VALUES(?,'2024-02-29')`, i)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			if err = tx.Commit(); err != nil {
				t.Fatal(err)
			}
			got, _, err := s.CalendarActivityRangeContext(context.Background(), "2024-02-29", "2024-02-29")
			if err != nil {
				t.Fatal(err)
			}
			if (kind == "tasks" && got[0].TaskCount != CalendarActivityRowLimit) || (kind == "notes" && got[0].NoteCount != CalendarActivityRowLimit) {
				t.Fatal(got)
			}
			if kind == "tasks" {
				_, err = s.db.Exec(`INSERT INTO tasks(note_id,line,state,due) VALUES(1,2,'TODO','2024-02-29')`)
			} else {
				_, err = s.db.Exec(`INSERT INTO note_days(note_id,day) VALUES(1,'2024-03-01')`)
			}
			if err != nil {
				t.Fatal(err)
			}
			if got, _, err := s.CalendarActivityRangeContext(context.Background(), "2024-02-29", "2024-03-01"); !errors.Is(err, ErrCalendarActivityUnavailable) || got != nil {
				t.Fatal(got, err)
			}
		})
	}
}

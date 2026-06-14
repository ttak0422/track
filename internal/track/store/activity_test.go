package store

import (
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/note"
)

func TestActivitySinceGroupsByLocalDay(t *testing.T) {
	s := newTestStore(t)
	day1 := time.Date(2026, 6, 10, 10, 0, 0, 0, time.Local)
	day2 := time.Date(2026, 6, 11, 9, 0, 0, 0, time.Local)
	for _, n := range []*note.Note{
		{ID: 100, Mtime: day1.Unix(), Meta: note.Metadata{Title: "A"}},
		{ID: 200, Mtime: day1.Add(2 * time.Hour).Unix(), Meta: note.Metadata{Title: "B"}},
		{ID: 300, Mtime: day2.Unix(), Meta: note.Metadata{Title: "C"}},
		{ID: 400, Mtime: day1.AddDate(0, 0, -3).Unix(), Meta: note.Metadata{Title: "Old"}},
	} {
		if err := s.UpsertNote(n); err != nil {
			t.Fatalf("upsert %d: %v", n.ID, err)
		}
	}

	got, err := s.ActivitySince(day1)
	if err != nil {
		t.Fatalf("activity: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("activity days = %+v, want 2 days", got)
	}
	if got[0].Date != "2026-06-10" || got[0].Count != 2 {
		t.Fatalf("first activity day = %+v", got[0])
	}
	if got[1].Date != "2026-06-11" || got[1].Count != 1 {
		t.Fatalf("second activity day = %+v", got[1])
	}
}

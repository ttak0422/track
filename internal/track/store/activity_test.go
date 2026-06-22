package store

import (
	"testing"

	"github.com/ttak0422/track/internal/track/note"
)

func TestNoteActivityRange(t *testing.T) {
	s := newTestStore(t)
	// Two notes active on 2026-06-10, one on 2026-06-11, and one outside the queried window.
	for _, n := range []*note.Note{
		{ID: 100, Meta: note.Metadata{Title: "A", Days: []string{"2026-06-10"}}},
		{ID: 200, Meta: note.Metadata{Title: "B", Days: []string{"2026-06-10", "2026-06-11"}}},
		{ID: 300, Meta: note.Metadata{Title: "C", Days: []string{"2026-06-11"}}},
		{ID: 400, Meta: note.Metadata{Title: "Old", Days: []string{"2026-06-01"}}},
		// A journal active in-window must not count: journals are excluded from note_days.
		{ID: 20260610, Kind: "journal", Meta: note.Metadata{Title: "20260610", Days: []string{"2026-06-10"}}},
	} {
		if err := s.UpsertNote(n); err != nil {
			t.Fatalf("upsert %d: %v", n.ID, err)
		}
	}

	got, err := s.NoteActivityRange("2026-06-10", "2026-06-11")
	if err != nil {
		t.Fatalf("activity: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("activity days = %+v, want 2 days", got)
	}
	if got[0].Date != "2026-06-10" || got[0].Count != 2 {
		t.Fatalf("first activity day = %+v, want 2026-06-10 count 2 (journal excluded)", got[0])
	}
	if got[1].Date != "2026-06-11" || got[1].Count != 2 {
		t.Fatalf("second activity day = %+v, want 2026-06-11 count 2", got[1])
	}
}

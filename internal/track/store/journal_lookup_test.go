package store

import (
	"context"
	"errors"
	"github.com/ttak0422/track/internal/track/note"
	"testing"
)

func TestJournalLookupExactKindAndAmbiguity(t *testing.T) {
	s := newTestStore(t)
	readyCalendarStore(t, s)
	for _, n := range []*note.Note{{ID: 20240229, Kind: "note", Meta: note.Metadata{Title: "20240229"}}, {ID: 901, Kind: "journal", Meta: note.Metadata{Title: "20240229"}}, {ID: 902, Kind: "journal", Meta: note.Metadata{Title: "202402"}}} {
		if err := s.UpsertNote(n); err != nil {
			t.Fatal(err)
		}
	}
	for title, id := range map[string]int64{"20240229": 901, "202402": 902} {
		found, revision, err := s.LookupJournalContext(context.Background(), title)
		if err != nil || found == nil || found.NoteID != id || found.FileKind != "journal" || found.Title != title {
			t.Fatal(found, err)
		}
		if err := s.CalendarActivityRevisionContext(context.Background(), revision); err != nil {
			t.Fatal(err)
		}
	}
	if found, _, err := s.LookupJournalContext(context.Background(), "202403"); err != nil || found != nil {
		t.Fatal(found, err)
	}
	if err := s.UpsertNote(&note.Note{ID: 903, Kind: "journal", Meta: note.Metadata{Title: "20240229"}}); err != nil {
		t.Fatal(err)
	}
	if found, _, err := s.LookupJournalContext(context.Background(), "20240229"); found != nil || !errors.Is(err, ErrJournalLookupAmbiguous) {
		t.Fatal(found, err)
	}
}
func TestJournalLookupValidationAndHealth(t *testing.T) {
	for _, title := range []string{"202402", "20240229", "000101", "99981231"} {
		if !ValidJournalLookupTitle(title) {
			t.Fatal(title)
		}
	}
	for _, title := range []string{"", "2024", "2024023", "202413", "20230229", "00000101", "999901", "２０２４０２", "202402 ", "../202402", "id:202402"} {
		if ValidJournalLookupTitle(title) {
			t.Fatal(title)
		}
	}
	s := newTestStore(t)
	if _, _, err := s.LookupJournalContext(context.Background(), "202402"); !errors.Is(err, ErrCalendarActivityUnavailable) {
		t.Fatal(err)
	}
	readyCalendarStore(t, s)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := s.LookupJournalContext(ctx, "202402"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := s.SetReadState(ReadState{CheckedAt: 1, Partial: true}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.LookupJournalContext(context.Background(), "202402"); !errors.Is(err, ErrCalendarActivityUnavailable) {
		t.Fatal(err)
	}
}

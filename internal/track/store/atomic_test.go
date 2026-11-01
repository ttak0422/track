package store

import (
	"github.com/ttak0422/track/internal/track/note"
	"testing"
)

func TestAtomicRebuildRollsBackRowsLinksAndStatus(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpsertNote(&note.Note{ID: 1, Meta: note.Metadata{Title: "Original"}}); err != nil {
		t.Fatal(err)
	}
	err := s.Atomic(func(tx *Store) error {
		if err := tx.UpsertNote(&note.Note{ID: 1, Meta: note.Metadata{Title: "Changed"}}); err != nil {
			return err
		}
		state := ReadState{CheckedAt: 42}
		state.Add(ReadIssue{Path: "synthetic", Reason: "injected", Cached: true})
		if err := tx.SetReadState(state); err != nil {
			return err
		}
		return tx.ReplaceLinks(1, []int64{999}) // Foreign-key failure after the row/status writes.
	})
	if err == nil {
		t.Fatal("injected DB failure did not fail")
	}
	if _, found, err := s.ResolveTerm("Original"); err != nil || !found {
		t.Fatalf("row escaped rollback: %v %v", found, err)
	}
	state, err := s.ReadState()
	if err != nil || state.Partial || state.CheckedAt != 0 {
		t.Fatalf("status escaped rollback: %+v %v", state, err)
	}
}

package query

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

func TestRowsFromStore(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	for _, n := range []*note.Note{
		{ID: 1, Mtime: 100, Meta: note.Metadata{Title: "Old", Tags: []string{"a/b"}}, Body: "status:: open"},
		{ID: 2, Mtime: 300, Meta: note.Metadata{Title: "New"}},
	} {
		if err := s.UpsertNote(n); err != nil {
			t.Fatalf("upsert %d: %v", n.ID, err)
		}
	}

	rows, err := RowsFromStore(s)
	if err != nil {
		t.Fatalf("rows: %v", err)
	}
	if got := []int64{rows[0].ID, rows[1].ID}; !reflect.DeepEqual(got, []int64{2, 1}) {
		t.Fatalf("order = %v, want most recently updated first [2 1]", got)
	}
	if !reflect.DeepEqual(rows[1].Tags, []string{"a/b"}) {
		t.Fatalf("tags = %v", rows[1].Tags)
	}
	// The indexed inline field is queryable.
	if got := values(rows[1], "status"); !reflect.DeepEqual(got, []string{"open"}) {
		t.Fatalf("props = %+v", rows[1].Props)
	}
}

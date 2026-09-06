package query

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

func TestRowsFromStoreMatchesInlineTags(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	for _, n := range []*note.Note{
		{ID: 1, Mtime: 100, Meta: note.Metadata{Title: "Track note"}, Body: "notes on #proj/track and #golang"},
		{ID: 2, Mtime: 200, Meta: note.Metadata{Title: "Other"}, Body: "unrelated"},
		{ID: 3, Mtime: 300, Meta: note.Metadata{Title: "Nested"}, Body: "deep in #proj/track/arch"},
	} {
		if err := s.UpsertNote(n); err != nil {
			t.Fatalf("upsert %d: %v", n.ID, err)
		}
	}

	rows, err := RowsFromStore(s)
	if err != nil {
		t.Fatalf("rows: %v", err)
	}

	// FROM #proj/track matches both the inline tag and its descendant, most recently updated first.
	q, err := Parse("TABLE title FROM #proj/track")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	res := Run(q, rows)
	if len(res.Rows) != 2 || res.Rows[0].Title != "Nested" || res.Rows[1].Title != "Track note" {
		t.Fatalf("FROM #proj/track rows = %+v, want [Nested Track note]", res.Rows)
	}

	// WHERE #golang matches the inline tag like any sidecar tag.
	q, err = Parse("TABLE title WHERE #golang")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res := Run(q, rows); len(res.Rows) != 1 || res.Rows[0].Title != "Track note" {
		t.Fatalf("WHERE #golang rows = %+v, want [Track note]", res.Rows)
	}

	// An absent tag stays empty.
	q, err = Parse("TABLE title FROM #missing")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res := Run(q, rows); len(res.Rows) != 0 {
		t.Fatalf("FROM #missing rows = %+v, want none", res.Rows)
	}
}

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
	// The indexed inline field is queryable as a property (props.<key>, never bare).
	if got := values(rows[1], "props.status"); !reflect.DeepEqual(got, []string{"open"}) {
		t.Fatalf("props = %+v", rows[1].Props)
	}
}

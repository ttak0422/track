package store

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/ttak0422/track/internal/track/note"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestUpsertAndKeywords(t *testing.T) {
	s := newTestStore(t)

	n := &note.Note{
		ID:    100,
		Path:  "/vault/100.md",
		Body:  "body",
		Mtime: 42,
		Meta: note.Metadata{
			Title:   "リンク",
			Tags:    []string{"zettel"},
			Created: "2026-05-24",
		},
	}
	if err := s.UpsertNote(n); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	kws, err := s.Keywords()
	if err != nil {
		t.Fatalf("keywords: %v", err)
	}
	terms := map[string]string{}
	for _, k := range kws {
		terms[k.Term] = k.Kind
		if k.NoteID != 100 || k.FileKind != "note" || k.Path != "" {
			t.Fatalf("unexpected keyword ref: %+v", k)
		}
	}
	for term, wantKind := range map[string]string{"リンク": "title"} {
		if terms[term] != wantKind {
			t.Fatalf("term %q kind = %q, want %q", term, terms[term], wantKind)
		}
	}
}

func TestResolveTerm(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpsertNote(&note.Note{ID: 7, Path: "/v/7.md", Meta: note.Metadata{Title: "Go"}}); err != nil {
		t.Fatal(err)
	}

	ref, found, err := s.ResolveTerm("Go")
	if err != nil || !found {
		t.Fatalf("resolve Go: found=%v err=%v", found, err)
	}
	if ref.NoteID != 7 || ref.Title != "Go" {
		t.Fatalf("unexpected ref: %+v", ref)
	}
	if ref.FileKind != "note" {
		t.Fatalf("unexpected file kind: %+v", ref)
	}

	_, found, err = s.ResolveTerm("missing")
	if err != nil {
		t.Fatalf("resolve missing: %v", err)
	}
	if found {
		t.Fatal("did not expect to resolve 'missing'")
	}
}

func TestLinksAndBacklinks(t *testing.T) {
	s := newTestStore(t)
	// Note 2 was updated more recently than 3, so it must list first (the shared recency order).
	for id, mtime := range map[int64]int64{1: 10, 2: 300, 3: 200} {
		if err := s.UpsertNote(&note.Note{ID: id, Path: fmt.Sprintf("/v/%d.md", id), Mtime: mtime, Meta: note.Metadata{Title: "n"}}); err != nil {
			t.Fatal(err)
		}
	}
	// 2 and 3 link to 1; self-link 1->1 is ignored.
	if err := s.ReplaceLinks(2, []int64{1}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceLinks(3, []int64{1}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceLinks(1, []int64{1}); err != nil {
		t.Fatal(err)
	}

	back, err := s.Backlinks(1)
	if err != nil {
		t.Fatalf("backlinks: %v", err)
	}
	if len(back) != 2 || back[0].NoteID != 2 || back[1].NoteID != 3 {
		t.Fatalf("backlinks should list most recently updated first: %+v", back)
	}
}

func TestDeleteNoteCascades(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpsertNote(&note.Note{ID: 5, Path: "/v/5.md", Meta: note.Metadata{Title: "t"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteNote(5); err != nil {
		t.Fatalf("delete: %v", err)
	}
	kws, _ := s.Keywords()
	if len(kws) != 0 {
		t.Fatalf("expected no keywords after delete, got %+v", kws)
	}
}

func TestNotesOnDay(t *testing.T) {
	s := newTestStore(t)
	// Note 1 carries an explicit activity day list; note 2 was updated more recently, so on their
	// shared day it lists first (the recency order every list surface uses).
	if err := s.UpsertNote(&note.Note{ID: 1, Path: "/v/1.md", Mtime: 100, Meta: note.Metadata{
		Title: "Listed", Created: "2026-06-20", Days: []string{"2026-06-20", "2026-06-22"},
	}}); err != nil {
		t.Fatal(err)
	}
	// Note 2 predates the days field: it falls back to its created day.
	if err := s.UpsertNote(&note.Note{ID: 2, Path: "/v/2.md", Mtime: 200, Meta: note.Metadata{
		Title: "Fallback", Created: "2026-06-22",
	}}); err != nil {
		t.Fatal(err)
	}
	// A journal active that day must NOT count as activity: journals are excluded from note_days.
	if err := s.UpsertNote(&note.Note{ID: 20260622, Kind: "journal", Path: "/v/journal/20260622.md", Meta: note.Metadata{
		Title: "20260622", Created: "2026-06-22", Days: []string{"2026-06-22"},
	}}); err != nil {
		t.Fatal(err)
	}

	on22, err := s.NotesOnDay("2026-06-22")
	if err != nil {
		t.Fatalf("notes on day: %v", err)
	}
	if len(on22) != 2 || on22[0].NoteID != 2 || on22[1].NoteID != 1 {
		t.Fatalf("NotesOnDay(2026-06-22) = %+v, want notes 2 then 1 (recency order, journal excluded)", on22)
	}

	on20, err := s.NotesOnDay("2026-06-20")
	if err != nil {
		t.Fatalf("notes on day: %v", err)
	}
	if len(on20) != 1 || on20[0].NoteID != 1 {
		t.Fatalf("NotesOnDay(2026-06-20) = %+v, want only note 1", on20)
	}

	empty, err := s.NotesOnDay("2026-01-01")
	if err != nil {
		t.Fatalf("notes on day: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("NotesOnDay with no matches = %+v, want empty", empty)
	}
}

func TestNoteMtimes(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpsertNote(&note.Note{ID: 9, Path: "/v/9.md", Mtime: 1234, Meta: note.Metadata{Title: "t"}}); err != nil {
		t.Fatal(err)
	}
	m, err := s.NoteMtimes()
	if err != nil {
		t.Fatalf("mtimes: %v", err)
	}
	if m[9] != 1234 {
		t.Fatalf("mtime[9] = %d, want 1234", m[9])
	}
}

func TestUpsertNoteIndexesProps(t *testing.T) {
	s := newTestStore(t)

	n := &note.Note{
		ID:   100,
		Path: "/vault/100.md",
		Body: "intro\nstatus:: draft\n- rating:: 8\n",
		Meta: note.Metadata{
			Title: "Props",
			Props: map[string]any{"owner": "[[Ada]]", "aliases": []any{"a", "b"}},
		},
	}
	if err := s.UpsertNote(n); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	props, err := s.NoteProps(100)
	if err != nil {
		t.Fatalf("note props: %v", err)
	}
	want := []note.Prop{
		{Key: "aliases", Value: "a", Type: "string", Line: 0},
		{Key: "aliases", Value: "b", Type: "string", Line: 0},
		{Key: "owner", Value: "Ada", Type: "link", Line: 0},
		{Key: "status", Value: "draft", Type: "string", Line: 2},
		{Key: "rating", Value: "8", Type: "number", Line: 3},
	}
	if len(props) != len(want) {
		t.Fatalf("props = %+v, want %+v", props, want)
	}
	for i := range want {
		if props[i] != want[i] {
			t.Fatalf("props[%d] = %+v, want %+v", i, props[i], want[i])
		}
	}

	// Re-upserting without props replaces the rows instead of accumulating them.
	n.Body = "plain body"
	n.Meta.Props = nil
	if err := s.UpsertNote(n); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	props, err = s.NoteProps(100)
	if err != nil {
		t.Fatalf("note props after re-upsert: %v", err)
	}
	if len(props) != 0 {
		t.Fatalf("props after re-upsert = %+v, want none", props)
	}
}

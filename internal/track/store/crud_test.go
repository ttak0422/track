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
			Aliases: []string{"link", "TEST"},
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
		if k.NoteID != 100 || k.Path != "" {
			t.Fatalf("unexpected keyword ref: %+v", k)
		}
	}
	for term, wantKind := range map[string]string{"リンク": "title", "link": "alias", "TEST": "alias"} {
		if terms[term] != wantKind {
			t.Fatalf("term %q kind = %q, want %q", term, terms[term], wantKind)
		}
	}
}

func TestUpsertReplacesAliases(t *testing.T) {
	s := newTestStore(t)
	base := &note.Note{ID: 1, Path: "/v/1.md", Meta: note.Metadata{Title: "t", Aliases: []string{"a", "b"}}}
	if err := s.UpsertNote(base); err != nil {
		t.Fatal(err)
	}
	base.Meta.Aliases = []string{"c"}
	if err := s.UpsertNote(base); err != nil {
		t.Fatal(err)
	}
	kws, _ := s.Keywords()
	for _, k := range kws {
		if k.Term == "a" || k.Term == "b" {
			t.Fatalf("stale alias %q still present", k.Term)
		}
	}
}

func TestResolveTerm(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpsertNote(&note.Note{ID: 7, Path: "/v/7.md", Meta: note.Metadata{Title: "Go", Aliases: []string{"golang"}}}); err != nil {
		t.Fatal(err)
	}

	ref, found, err := s.ResolveTerm("golang")
	if err != nil || !found {
		t.Fatalf("resolve golang: found=%v err=%v", found, err)
	}
	if ref.NoteID != 7 || ref.Title != "Go" {
		t.Fatalf("unexpected ref: %+v", ref)
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
	for _, id := range []int64{1, 2, 3} {
		if err := s.UpsertNote(&note.Note{ID: id, Path: fmt.Sprintf("/v/%d.md", id), Meta: note.Metadata{Title: "n"}}); err != nil {
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
		t.Fatalf("unexpected backlinks: %+v", back)
	}
}

func TestDeleteNoteCascades(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpsertNote(&note.Note{ID: 5, Path: "/v/5.md", Meta: note.Metadata{Title: "t", Aliases: []string{"a"}}}); err != nil {
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

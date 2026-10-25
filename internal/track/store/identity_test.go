package store

import (
	"github.com/ttak0422/track/internal/track/note"
	"testing"
)

func TestResolveLinkIdentityAndLegacySeparation(t *testing.T) {
	s := newTestStore(t)
	for id, title := range map[int64]string{1: "Old title", 2: "1", 3: "id:999", 4: "id:invalid"} {
		if err := s.UpsertNote(&note.Note{ID: id, Meta: note.Metadata{Title: title}}); err != nil {
			t.Fatal(err)
		}
	}
	for _, tt := range []struct {
		key, format string
		id          int64
		found       bool
	}{
		{"id:1", "id-v1", 1, true}, {"1", "id-v1", 2, true}, {"Old title", "id-v1", 1, true},
		{"id:998", "id-v1", 0, false}, {"id:missing", "id-v1", 0, false},
		{"id:999", "", 3, true}, {"id:invalid", "title", 4, true},
	} {
		ref, found, err := s.ResolveLink(tt.key, tt.format)
		if err != nil || found != tt.found || ref.NoteID != tt.id {
			t.Fatalf("%+v => %+v %v %v", tt, ref, found, err)
		}
	}
	for _, key := range []string{"id:999", "id:invalid"} {
		if _, found, err := s.ResolveLink(key, "id-v1"); err == nil || found {
			t.Fatalf("literal collision must fail closed: %s %v %v", key, found, err)
		}
	}
	if _, err := s.LinkKeywords("id-v1"); err == nil {
		t.Fatal("runtime namespace collision accepted")
	}
	if err := s.UpsertNote(&note.Note{ID: 1, Meta: note.Metadata{Title: "Renamed"}}); err != nil {
		t.Fatal(err)
	}
	ref, found, err := s.ResolveLink("id:1", "id-v1")
	if err != nil || !found || ref.Title != "Renamed" {
		t.Fatalf("rename changed identity: %+v %v", ref, err)
	}
}

func TestIdentityHierarchyUsesIndexedSourceMode(t *testing.T) {
	s := newTestStore(t)
	upsert(t, s, 1, "Root", "", 1)
	upsert(t, s, 2, "Child", "up:: [[id:1|Old title]]", 2)
	if parents, err := s.UpNotes(2); err != nil || len(parents) != 0 {
		t.Fatalf("legacy ID parent %+v %v", parents, err)
	}
	if err := s.SetIndexedLinkFormat("id-v1"); err != nil {
		t.Fatal(err)
	}
	if parents, err := s.UpNotes(2); err != nil || len(parents) != 1 || parents[0].NoteID != 1 {
		t.Fatalf("ID parent %+v %v", parents, err)
	}
	if children, err := s.ChildNotes(1); err != nil || len(children) != 1 || children[0].NoteID != 2 {
		t.Fatalf("ID child %+v %v", children, err)
	}
	if roots, err := s.Hierarchy(); err != nil || len(roots) != 1 || roots[0].NoteID != 1 || len(roots[0].Children) != 1 {
		t.Fatalf("ID hierarchy %+v %v", roots, err)
	}
	if trail, err := s.Trail(2); err != nil || len(trail) != 1 || trail[0].NoteID != 1 {
		t.Fatalf("ID trail %+v %v", trail, err)
	}
	if _, found, err := s.ResolveTerm("id:1"); err != nil || found {
		t.Fatalf("ID leaked into title uniqueness dictionary: %v %v", found, err)
	}
}

func TestExternalIdentityIsPersistedSeparateFromLegacyTitle(t *testing.T) {
	s := newTestStore(t)
	upsert(t, s, 7, "Legacy source", "", 1)
	upsert(t, s, 8, "Typed source", "", 1)
	legacy, ok := NewExtRef("work", "id:1", "")
	if !ok || legacy.TargetID != 0 || legacy.Title != "id:1" {
		t.Fatalf("legacy %+v", legacy)
	}
	typed, ok := NewExtRef("work", "id:1", "id-v1")
	if !ok || typed.TargetID != 1 || typed.Title != "" {
		t.Fatalf("typed %+v", typed)
	}
	if _, ok := NewExtRef("work", "id:bad", "id-v1"); ok {
		t.Fatal("malformed explicit ID treated as title")
	}
	if err := s.ReplaceExtLinks(7, []ExtRef{legacy}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceExtLinks(8, []ExtRef{typed}); err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"", "id-v1", "title"} {
		if err := s.SetIndexedLinkFormat(format); err != nil {
			t.Fatal(err)
		}
		backs, err := s.ExtBacklinksTo([]string{"work"}, "Renamed", 1)
		if err != nil || len(backs) != 1 || backs[0].NoteID != 8 {
			t.Fatalf("typed edge reinterpreted in %q: %+v %v", format, backs, err)
		}
		backs, err = s.ExtBacklinksTo([]string{"work"}, "id:1", 2)
		if err != nil || len(backs) != 1 || backs[0].NoteID != 7 {
			t.Fatalf("legacy title reinterpreted in %q: %+v %v", format, backs, err)
		}
	}
}

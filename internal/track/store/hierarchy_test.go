package store

import (
	"testing"

	"github.com/ttak0422/track/internal/track/note"
)

// upsert adds a titled note whose body carries the given inline fields, so props index normally.
func upsert(t *testing.T, s *Store, id int64, title, body string, mtime int64) {
	t.Helper()
	n := &note.Note{ID: id, Body: body, Mtime: mtime, Meta: note.Metadata{Title: title}}
	if err := s.UpsertNote(n); err != nil {
		t.Fatalf("upsert %d: %v", id, err)
	}
}

func TestHierarchyTrailAndChildren(t *testing.T) {
	s := newTestStore(t)
	upsert(t, s, 1, "Root", "", 10)
	upsert(t, s, 2, "Mid", "up:: [[Root]]", 20)
	upsert(t, s, 3, "Leaf", "up:: [[Mid]]", 30)
	upsert(t, s, 4, "Leaf 2", "up:: [[Mid]]", 40)
	// A non-link "up" value is not a parent.
	upsert(t, s, 5, "Stray", "up:: somewhere", 50)

	trail, err := s.Trail(3)
	if err != nil {
		t.Fatalf("trail: %v", err)
	}
	if len(trail) != 2 || trail[0].Title != "Root" || trail[1].Title != "Mid" {
		t.Fatalf("trail = %+v, want Root then Mid", trail)
	}

	children, err := s.ChildNotes(2)
	if err != nil {
		t.Fatalf("children: %v", err)
	}
	// Shared note-list order: most recently updated first.
	if len(children) != 2 || children[0].Title != "Leaf 2" || children[1].Title != "Leaf" {
		t.Fatalf("children = %+v, want Leaf 2 then Leaf", children)
	}

	if kids, _ := s.ChildNotes(5); len(kids) != 0 {
		t.Fatalf("string-valued up must not create children: %+v", kids)
	}
	if up, _ := s.UpNotes(1); len(up) != 0 {
		t.Fatalf("root has no parents, got %+v", up)
	}
}

func TestHierarchyTrailStopsOnCycle(t *testing.T) {
	s := newTestStore(t)
	upsert(t, s, 1, "A", "up:: [[B]]", 10)
	upsert(t, s, 2, "B", "up:: [[A]]", 20)

	trail, err := s.Trail(1)
	if err != nil {
		t.Fatalf("trail: %v", err)
	}
	if len(trail) != 1 || trail[0].Title != "B" {
		t.Fatalf("cyclic trail should stop after B, got %+v", trail)
	}
}

func TestUpNotesFollowsFirstPropertyOrder(t *testing.T) {
	s := newTestStore(t)
	upsert(t, s, 1, "P1", "", 10)
	upsert(t, s, 2, "P2", "", 20)
	upsert(t, s, 3, "C", "up:: [[P2]], [[P1]]", 30)

	up, err := s.UpNotes(3)
	if err != nil {
		t.Fatalf("up: %v", err)
	}
	if len(up) != 2 || up[0].Title != "P2" || up[1].Title != "P1" {
		t.Fatalf("parents = %+v, want P2 then P1 (property order)", up)
	}
	trail, err := s.Trail(3)
	if err != nil {
		t.Fatalf("trail: %v", err)
	}
	if len(trail) != 1 || trail[0].Title != "P2" {
		t.Fatalf("trail follows the first parent, got %+v", trail)
	}
}

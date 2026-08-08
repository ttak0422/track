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

func TestHierarchyForest(t *testing.T) {
	s := newTestStore(t)
	upsert(t, s, 1, "Root", "", 10)
	upsert(t, s, 2, "Mid", "up:: [[Root]]", 20)
	upsert(t, s, 3, "Leaf", "up:: [[Mid]]", 30)
	upsert(t, s, 4, "Leaf 2", "up:: [[Mid]]", 40)
	// Placed by nothing: no parent, no children. It is not a root, it is simply not in the tree.
	upsert(t, s, 5, "Stray", "up:: somewhere", 50)
	// A second root, more recently updated but earlier by title, so the ordering is pinned to the
	// title rather than to the mtime every other listing sorts by.
	upsert(t, s, 6, "Other root", "", 60)
	upsert(t, s, 7, "Other child", "up:: [[Other root]]", 70)

	roots, err := s.Hierarchy()
	if err != nil {
		t.Fatalf("hierarchy: %v", err)
	}
	if len(roots) != 2 || roots[0].Title != "Other root" || roots[1].Title != "Root" {
		t.Fatalf("roots = %s, want Other root then Root", titles(roots))
	}
	mid := roots[1].Children
	if len(mid) != 1 || mid[0].Title != "Mid" {
		t.Fatalf("Root's children = %s, want Mid", titles(mid))
	}
	// By title, so the newer "Leaf 2" sorts after "Leaf" instead of ahead of it.
	if kids := mid[0].Children; len(kids) != 2 || kids[0].Title != "Leaf" || kids[1].Title != "Leaf 2" {
		t.Fatalf("Mid's children = %s, want Leaf then Leaf 2", titles(kids))
	}
}

func TestHierarchyDropsCycles(t *testing.T) {
	s := newTestStore(t)
	upsert(t, s, 1, "A", "up:: [[B]]", 10)
	upsert(t, s, 2, "B", "up:: [[A]]", 20)
	upsert(t, s, 3, "Root", "", 30)
	upsert(t, s, 4, "Child", "up:: [[Root]]", 40)

	roots, err := s.Hierarchy()
	if err != nil {
		t.Fatalf("hierarchy: %v", err)
	}
	// Every member of a cycle has a parent, so none is a root and the whole loop stays out of the
	// forest — which is what keeps a consumer's walk finite.
	if len(roots) != 1 || roots[0].Title != "Root" {
		t.Fatalf("roots = %s, want Root alone", titles(roots))
	}
}

func titles(nodes []*HierarchyNode) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.Title)
	}
	return out
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

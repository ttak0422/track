package store

import (
	"testing"

	"github.com/ttak0422/track/internal/track/note"
)

func TestFullGraph(t *testing.T) {
	s := newTestStore(t)
	for _, n := range []*note.Note{
		{ID: 1, Path: "/v/1.md", Meta: note.Metadata{Title: "A"}},
		{ID: 2, Path: "/v/2.md", Meta: note.Metadata{Title: "B"}},
		{ID: 3, Path: "/v/3.md", Meta: note.Metadata{Title: "C"}},
	} {
		if err := s.UpsertNote(n); err != nil {
			t.Fatalf("upsert %d: %v", n.ID, err)
		}
	}
	if err := s.ReplaceLinks(1, []int64{2}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceLinks(2, []int64{3}); err != nil {
		t.Fatal(err)
	}

	g, err := s.FullGraph()
	if err != nil {
		t.Fatalf("full graph: %v", err)
	}
	if g.CenterID != 0 {
		t.Fatalf("full graph should have no center, got %d", g.CenterID)
	}
	if len(g.Nodes) != 3 {
		t.Fatalf("nodes = %d, want 3", len(g.Nodes))
	}
	if len(g.Edges) != 2 {
		t.Fatalf("edges = %d, want 2: %+v", len(g.Edges), g.Edges)
	}
}

func TestFullGraphEmpty(t *testing.T) {
	s := newTestStore(t)
	g, err := s.FullGraph()
	if err != nil {
		t.Fatalf("full graph: %v", err)
	}
	if g.Nodes == nil || g.Edges == nil {
		t.Fatalf("empty graph should return non-nil slices, got %+v", g)
	}
	if len(g.Nodes) != 0 || len(g.Edges) != 0 {
		t.Fatalf("empty graph should be empty, got %+v", g)
	}
}

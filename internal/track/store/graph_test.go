package store

import (
	"fmt"
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

	// Sizes are the absolute five-level grade of each note's outgoing links: A writes 1 link (level
	// 2), B writes 1 (level 2), C writes none (level 1).
	sizes := map[int64]int{}
	for _, n := range g.Nodes {
		sizes[n.NoteID] = n.Size
	}
	for id, want := range map[int64]int{1: 2, 2: 2, 3: 1} {
		if sizes[id] != want {
			t.Errorf("note %d size = %d, want %d", id, sizes[id], want)
		}
	}

	// A note that links a lot grades higher: 8 links is the top level, and the local graph around
	// an unrelated note still reports the same absolute grade.
	if err := s.ReplaceLinks(2, []int64{3, 1, 3, 1, 3, 1, 3, 1}); err != nil {
		t.Fatal(err)
	}
	// (ReplaceLinks stores a set, so the repeated targets collapse; add distinct notes instead.)
	extra := []int64{4, 5, 6, 7, 8, 9, 10}
	for _, id := range extra {
		if err := s.UpsertNote(&note.Note{ID: id, Path: fmt.Sprintf("/v/%d.md", id), Meta: note.Metadata{Title: fmt.Sprintf("N%d", id)}}); err != nil {
			t.Fatalf("upsert %d: %v", id, err)
		}
	}
	if err := s.ReplaceLinks(2, append([]int64{1, 3}, extra...)); err != nil {
		t.Fatal(err)
	}
	full, err := s.FullGraph()
	if err != nil {
		t.Fatalf("full graph: %v", err)
	}
	local, err := s.LocalGraph(3)
	if err != nil {
		t.Fatalf("local graph: %v", err)
	}
	for _, g2 := range []Graph{full, local} {
		byID := map[int64]int{}
		for _, n := range g2.Nodes {
			byID[n.NoteID] = n.Size
		}
		if byID[2] != 5 {
			t.Errorf("note 2 (9 links) size = %d, want 5 in %+v", byID[2], g2)
		}
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

func TestOrphans(t *testing.T) {
	s := newTestStore(t)
	for _, n := range []*note.Note{
		{ID: 1, Path: "/v/1.md", Meta: note.Metadata{Title: "A"}},      // orphan: no inbound link
		{ID: 2, Path: "/v/2.md", Meta: note.Metadata{Title: "B"}},      // linked from A
		{ID: 3, Path: "/v/3.md", Meta: note.Metadata{Title: "parent"}}, // orphan, but owns a child
		{ID: 4, Path: "/v/4.md", Meta: note.Metadata{Title: "parent / child"}},
		{ID: 5, Path: "/v/5.md", Meta: note.Metadata{Title: "foo / bar"}}, // dangling: no "foo" note
	} {
		if err := s.UpsertNote(n); err != nil {
			t.Fatalf("upsert %d: %v", n.ID, err)
		}
	}
	if err := s.ReplaceLinks(1, []int64{2}); err != nil {
		t.Fatal(err)
	}

	rep, err := s.Orphans()
	if err != nil {
		t.Fatalf("orphans: %v", err)
	}

	orphanIDs := map[int64]bool{}
	for _, o := range rep.Orphans {
		orphanIDs[o.NoteID] = true
	}
	// 1, 3, 4, 5 have no inbound link; 2 is linked from 1.
	for _, id := range []int64{1, 3, 4, 5} {
		if !orphanIDs[id] {
			t.Errorf("note %d should be an orphan; got %+v", id, rep.Orphans)
		}
	}
	if orphanIDs[2] {
		t.Errorf("note 2 has an inbound link, should not be an orphan")
	}

	if len(rep.DanglingPrefixes) != 1 || rep.DanglingPrefixes[0].NoteID != 5 || rep.DanglingPrefixes[0].MissingParent != "foo" {
		t.Fatalf("dangling = %+v, want only note 5 missing parent foo", rep.DanglingPrefixes)
	}
}

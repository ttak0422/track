package store

import (
	"fmt"
	"math"
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

// fullGraphTestStore builds a small vault with two components — a chain 1→2→3→4 and a star 5→(6,7,8)
// — plus the isolated note 9, so layout tests can exercise hubs, chains and singletons at once.
func fullGraphTestStore(t *testing.T) *Store {
	t.Helper()
	s := newTestStore(t)
	for id := int64(1); id <= 9; id++ {
		n := &note.Note{ID: id, Path: fmt.Sprintf("/v/%d.md", id), Meta: note.Metadata{Title: fmt.Sprintf("N%d", id)}}
		if err := s.UpsertNote(n); err != nil {
			t.Fatalf("upsert %d: %v", id, err)
		}
	}
	if err := s.ReplaceLinks(1, []int64{2}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceLinks(2, []int64{3}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceLinks(3, []int64{4}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceLinks(5, []int64{6, 7, 8}); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestFullGraphLayout(t *testing.T) {
	s := fullGraphTestStore(t)
	g, err := s.FullGraph()
	if err != nil {
		t.Fatalf("full graph: %v", err)
	}

	byID := map[int64]GraphNode{}
	for _, n := range g.Nodes {
		byID[n.NoteID] = n
		if math.IsNaN(n.X) || math.IsInf(n.X, 0) || math.IsNaN(n.Y) || math.IsInf(n.Y, 0) {
			t.Fatalf("node %d has non-finite coordinates (%v, %v)", n.NoteID, n.X, n.Y)
		}
		if n.X <= 0 || n.Y <= 0 {
			t.Errorf("node %d coordinates must stay positive so JSON omitempty keeps them, got (%v, %v)", n.NoteID, n.X, n.Y)
		}
	}

	// Every edge's both ends exist among the laid-out nodes.
	for _, e := range g.Edges {
		if _, ok := byID[e.SourceID]; !ok {
			t.Errorf("edge source %d is not a node", e.SourceID)
		}
		if _, ok := byID[e.TargetID]; !ok {
			t.Errorf("edge target %d is not a node", e.TargetID)
		}
	}

	// No two notes share a position: an overview where nodes stack hides the structure it exists to show.
	type pt struct{ x, y float64 }
	seen := map[pt]int64{}
	for _, n := range g.Nodes {
		p := pt{n.X, n.Y}
		if prev, dup := seen[p]; dup {
			t.Errorf("nodes %d and %d share position (%v, %v)", prev, n.NoteID, n.X, n.Y)
		}
		seen[p] = n.NoteID
	}

	// The star's hub (5, degree 3) sits at its component center: its leaves are equidistant on one
	// ring, and no leaf is closer to the component's other notes than to the hub.
	hub, ring := byID[5], make(map[int64]float64, 3)
	for _, leaf := range []int64{6, 7, 8} {
		dx, dy := byID[leaf].X-hub.X, byID[leaf].Y-hub.Y
		ring[leaf] = math.Sqrt(dx*dx + dy*dy)
	}
	for _, leaf := range []int64{6, 7, 8} {
		if math.Abs(ring[leaf]-ring[6]) > 1e-9 {
			t.Errorf("star leaf %d sits at radius %v, want the hub ring %v", leaf, ring[leaf], ring[6])
		}
	}

	// The local graph carries no layout: it never sets x/y.
	local, err := s.LocalGraph(2)
	if err != nil {
		t.Fatalf("local graph: %v", err)
	}
	for _, n := range local.Nodes {
		if n.X != 0 || n.Y != 0 {
			t.Errorf("local graph node %d should have no layout, got (%v, %v)", n.NoteID, n.X, n.Y)
		}
	}
}

func TestFullGraphLayoutDeterministic(t *testing.T) {
	s := fullGraphTestStore(t)
	first, err := s.FullGraph()
	if err != nil {
		t.Fatalf("full graph: %v", err)
	}
	second, err := s.FullGraph()
	if err != nil {
		t.Fatalf("full graph (2nd): %v", err)
	}
	if len(first.Nodes) != len(second.Nodes) {
		t.Fatalf("node counts differ between runs: %d vs %d", len(first.Nodes), len(second.Nodes))
	}
	for i := range first.Nodes {
		a, b := first.Nodes[i], second.Nodes[i]
		if a.NoteID != b.NoteID || a.X != b.X || a.Y != b.Y {
			t.Fatalf("layout is not deterministic at node %d: (%v, %v) vs (%v, %v)", a.NoteID, a.X, a.Y, b.X, b.Y)
		}
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

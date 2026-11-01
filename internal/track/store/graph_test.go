package store

import (
	"encoding/json"
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
		areas := map[int64]float64{}
		for _, n := range g2.Nodes {
			byID[n.NoteID] = n.Size
			areas[n.NoteID] = n.AreaScore
		}
		if byID[2] != 5 {
			t.Errorf("note 2 (9 links) size = %d, want 5 in %+v", byID[2], g2)
		}
		// The additive area score uses the whole authorized vault, before local slicing.
		if want := 1 + 4*81.0/181; math.Abs(areas[2]-want) > 1e-12 {
			t.Errorf("note 2 area = %g, want %g", areas[2], want)
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

// Synthetic link-count distributions only; no user's note bodies are read.
func TestGraphSizes(t *testing.T) {
	for _, counts := range [][]int{
		{}, {0}, {12}, {0, 0, 0}, {12, 12, 12}, {8, 16, 32, 64, 128},
		{1, 2, 4, 8, 1000000000}, {80, 81, 82, 83},
	} {
		t.Run(fmt.Sprint(counts), func(t *testing.T) {
			notes, byID := sizeFixture(counts)
			sizes := graphSizes(notes, byID)
			if len(sizes) != len(counts) {
				t.Fatalf("sizes = %v", sizes)
			}
			for i, count := range counts {
				got := sizes[int64(i+1)]
				if math.IsNaN(got) || got < 1 || got > 5 {
					t.Fatalf("invalid size %g", got)
				}
				if count == 0 && got != 1 {
					t.Fatalf("zero count size %g", got)
				}
				for j, other := range counts {
					if count == other && got != sizes[int64(j+1)] {
						t.Fatal("unequal ties")
					}
					if count < other && got >= sizes[int64(j+1)] {
						t.Fatal("not monotonic")
					}
				}
			}
			// Reordering (or redrawing) cannot change the reference.
			for i, j := 0, len(notes)-1; i < j; i, j = i+1, j-1 {
				notes[i], notes[j] = notes[j], notes[i]
			}
			for id, want := range sizes {
				if graphSizes(notes, byID)[id] != want {
					t.Fatal("order-dependent size")
				}
			}
		})
	}
}

func TestGraphSizesReference(t *testing.T) {
	notes, counts := sizeFixture([]int{8, 16, 32, 64, 128})
	base := graphSizes(notes, counts)
	// Adding/removing zeros or an unindexed source does not move the reference.
	counts[100] = 1000000000
	withZero := append(append([]SearchResult{}, notes...), SearchResult{NoteID: 101})
	for id, want := range base {
		if got := graphSizes(withZero, counts)[id]; got != want {
			t.Fatalf("zero/stale source moved size: %g != %g", got, want)
		}
	}
	// A single extreme outlier does not flatten the ordinary range.
	notes, counts = sizeFixture([]int{8, 16, 32, 64, 1000000000})
	outlier := graphSizes(notes, counts)
	for _, id := range []int64{1, 2, 3, 4} {
		if outlier[id] != base[id] {
			t.Fatal("outlier magnitude moved the median")
		}
	}
	// A changed population intentionally recomputes the reference; removing it restores the sizes.
	notes = append(notes, SearchResult{NoteID: 6})
	counts[6] = 256
	if graphSizes(notes, counts)[2] == outlier[2] {
		t.Fatal("addition did not recompute reference")
	}
	delete(counts, 6)
	if graphSizes(notes[:5], counts)[2] != outlier[2] {
		t.Fatal("removal did not restore reference")
	}
	// Proportional growth preserves the shape; tiny differences stay tiny.
	largeNotes, largeCounts := sizeFixture([]int{80, 160, 320, 640, 1280})
	for id, want := range base {
		if got := graphSizes(largeNotes, largeCounts)[id]; math.Abs(got-want) > 1e-12 {
			t.Fatal("scale-dependent shape")
		}
	}
	nearNotes, nearCounts := sizeFixture([]int{80, 81, 82, 83})
	near := graphSizes(nearNotes, nearCounts)
	if near[4]-near[1] > 0.1 {
		t.Fatal("tiny differences stretched across scale")
	}
}

func sizeFixture(counts []int) ([]SearchResult, map[int64]int) {
	notes := make([]SearchResult, len(counts))
	byID := make(map[int64]int, len(counts))
	for i, count := range counts {
		id := int64(i + 1)
		notes[i] = SearchResult{NoteID: id}
		byID[id] = count
	}
	return notes, byID
}

func TestGraphLegacySizeJSON(t *testing.T) {
	for count, want := range map[int]int{0: 1, 1: 2, 2: 3, 3: 3, 4: 4, 7: 4, 8: 5, 100: 5} {
		if got := GraphSizeLevel(count); got != want {
			t.Fatalf("count %d grade %d, want %d", count, got, want)
		}
	}
	payload, err := json.Marshal(Graph{Nodes: []GraphNode{{NoteID: 1, Size: 2, AreaScore: 1.8}}})
	if err != nil {
		t.Fatal(err)
	}
	// An old integer-only decoder ignores the additive field, even before its client is updated.
	var old struct {
		Nodes []struct {
			Size int `json:"size"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(payload, &old); err != nil {
		t.Fatal(err)
	}
	if old.Nodes[0].Size != 2 {
		t.Fatal("legacy grade changed")
	}
	var current Graph
	if err := json.Unmarshal(payload, &current); err != nil {
		t.Fatal(err)
	}
	if current.Nodes[0].AreaScore != 1.8 {
		t.Fatal("area score lost on the wire")
	}
}

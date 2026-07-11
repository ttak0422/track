package store

import (
	"slices"
	"testing"

	"github.com/ttak0422/track/internal/track/note"
)

func TestSearchRanksByTitleMatchMtimeAndID(t *testing.T) {
	s := newTestStore(t)
	for _, n := range []*note.Note{
		{
			ID:    100,
			Mtime: 100,
			Meta:  note.Metadata{Title: "Topic old"},
		},
		{
			ID:    200,
			Mtime: 300,
			Meta:  note.Metadata{Title: "Topic recent lower id"},
		},
		{
			ID:    300,
			Mtime: 300,
			Meta:  note.Metadata{Title: "Topic recent higher id"},
		},
	} {
		if err := s.UpsertNote(n); err != nil {
			t.Fatalf("upsert %d: %v", n.ID, err)
		}
	}

	results, err := s.SearchScoped("Topic", 10, SearchTitle)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %+v", results)
	}
	gotIDs := []int64{results[0].NoteID, results[1].NoteID, results[2].NoteID}
	wantIDs := []int64{300, 200, 100}
	for i := range wantIDs {
		if gotIDs[i] != wantIDs[i] {
			t.Fatalf("result order = %v, want %v", gotIDs, wantIDs)
		}
	}
}

func TestSearchRefsCarriesIconOverride(t *testing.T) {
	s := newTestStore(t)
	for _, n := range []*note.Note{
		{ID: 100, Mtime: 100, Meta: note.Metadata{Title: "Iconed", Icon: "🔥"}},
		{ID: 200, Mtime: 200, Meta: note.Metadata{Title: "Plain"}},
	} {
		if err := s.UpsertNote(n); err != nil {
			t.Fatalf("upsert: %v", err)
		}
	}
	refs, err := s.SearchRefs()
	if err != nil {
		t.Fatalf("search refs: %v", err)
	}
	got := map[string]string{}
	for _, r := range refs {
		got[r.Title] = r.Icon
	}
	if got["Iconed"] != "🔥" {
		t.Errorf("icon override lost through index/search: %q", got["Iconed"])
	}
	if got["Plain"] != "" {
		t.Errorf("note without an override should carry no icon, got %q", got["Plain"])
	}
}

func TestSearchTitleAndOr(t *testing.T) {
	s := newTestStore(t)
	for _, n := range []*note.Note{
		{ID: 100, Mtime: 100, Meta: note.Metadata{Title: "Alpha Beta"}},
		{ID: 200, Mtime: 200, Meta: note.Metadata{Title: "Alpha Gamma"}},
		{ID: 300, Mtime: 300, Meta: note.Metadata{Title: "Delta"}},
	} {
		if err := s.UpsertNote(n); err != nil {
			t.Fatalf("upsert %d: %v", n.ID, err)
		}
	}

	ids := func(query string) []int64 {
		t.Helper()
		results, err := s.SearchScoped(query, 10, SearchTitle)
		if err != nil {
			t.Fatalf("search %q: %v", query, err)
		}
		got := make([]int64, len(results))
		for i, r := range results {
			got[i] = r.NoteID
		}
		slices.Sort(got)
		return got
	}

	if got := ids("Alpha Beta"); !slices.Equal(got, []int64{100}) {
		t.Fatalf("implicit AND = %v, want [100]", got)
	}
	if got := ids("Alpha AND Gamma"); !slices.Equal(got, []int64{200}) {
		t.Fatalf("explicit AND = %v, want [200]", got)
	}
	if got := ids("Beta OR Delta"); !slices.Equal(got, []int64{100, 300}) {
		t.Fatalf("OR = %v, want [100 300]", got)
	}
	if got := ids("Alpha Beta OR Delta"); !slices.Equal(got, []int64{100, 300}) {
		t.Fatalf("grouped (Alpha AND Beta) OR Delta = %v, want [100 300]", got)
	}
	if got := ids("Alpha"); !slices.Equal(got, []int64{100, 200}) {
		t.Fatalf("single term = %v, want [100 200]", got)
	}
}

func TestSearchHashPrefixMatchesTags(t *testing.T) {
	s := newTestStore(t)
	for _, n := range []*note.Note{
		{
			ID:    100,
			Mtime: 100,
			Meta:  note.Metadata{Title: "Exact old", Tags: []string{"graph"}},
		},
		{
			ID:    200,
			Mtime: 300,
			Meta:  note.Metadata{Title: "Exact recent", Tags: []string{"graph", "draft"}},
		},
		{
			ID:    300,
			Mtime: 500,
			Meta:  note.Metadata{Title: "Prefix", Tags: []string{"graph-workspace"}},
		},
		{
			ID:    400,
			Mtime: 900,
			Meta:  note.Metadata{Title: "Substring", Tags: []string{"cartography"}},
		},
	} {
		if err := s.UpsertNote(n); err != nil {
			t.Fatalf("upsert %d: %v", n.ID, err)
		}
	}

	results, err := s.SearchScoped("#graph", 10, SearchTitle)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %+v", results)
	}
	gotIDs := []int64{results[0].NoteID, results[1].NoteID, results[2].NoteID, results[3].NoteID}
	wantIDs := []int64{200, 100, 300, 400}
	for i := range wantIDs {
		if gotIDs[i] != wantIDs[i] {
			t.Fatalf("result order = %v, want %v", gotIDs, wantIDs)
		}
	}
	if !slices.Contains(results[0].Tags, "graph") || !slices.Contains(results[0].Tags, "draft") {
		t.Fatalf("expected tag metadata on result: %+v", results[0])
	}
}

func TestSearchHashPrefixCombinesMultipleTagsAndTitleText(t *testing.T) {
	s := newTestStore(t)
	for _, n := range []*note.Note{
		{
			ID:    100,
			Mtime: 100,
			Meta:  note.Metadata{Title: "Graph Workspace", Tags: []string{"graph", "web"}},
		},
		{
			ID:    200,
			Mtime: 200,
			Meta:  note.Metadata{Title: "Graph Draft", Tags: []string{"graph", "draft"}},
		},
		{
			ID:    300,
			Mtime: 300,
			Meta:  note.Metadata{Title: "Web Workspace", Tags: []string{"web", "workspace"}},
		},
	} {
		if err := s.UpsertNote(n); err != nil {
			t.Fatalf("upsert %d: %v", n.ID, err)
		}
	}

	results, err := s.SearchScoped("#graph #web", 10, SearchTitle)
	if err != nil {
		t.Fatalf("search tags: %v", err)
	}
	if len(results) != 1 || results[0].NoteID != 100 {
		t.Fatalf("multi-tag results = %+v, want only note 100", results)
	}

	results, err = s.SearchScoped("#graph Workspace", 10, SearchTitle)
	if err != nil {
		t.Fatalf("search tag plus text: %v", err)
	}
	if len(results) != 1 || results[0].NoteID != 100 {
		t.Fatalf("tag plus text results = %+v, want only note 100", results)
	}
}

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

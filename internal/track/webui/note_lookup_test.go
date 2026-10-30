package webui

import (
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

func noteLookupView(t *testing.T) *vaultView {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return &vaultView{store: s}
}

func TestNoteByIDMatchesSearchRefs(t *testing.T) {
	v := noteLookupView(t)
	notes := []*note.Note{
		{ID: 100, Kind: "note", Mtime: 42, Body: "Body #inline", Meta: note.Metadata{
			Title: "日本語のノート", Tags: []string{"z", "alpha", "日本語"}, Icon: "🌱",
			Flags:  []string{"CONFIDENTIAL", "DEPRECATED"},
			SeenAt: "2026-10-28T01:00:00Z", ReadAt: "2026-10-28T02:00:00Z",
		}},
		{ID: 200, Meta: note.Metadata{Title: "Plain"}},
		{ID: 20261030, Kind: "journal", Mtime: 77, Meta: note.Metadata{Title: "20261030"}},
		{ID: 300, Kind: "template", Meta: note.Metadata{Title: "Template"}},
	}
	for _, n := range notes {
		if err := v.store.UpsertNote(n); err != nil {
			t.Fatal(err)
		}
	}
	check := func() {
		t.Helper()
		refs, err := v.store.SearchRefs()
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range refs {
			got, err := v.noteByID(want.NoteID)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("lookup %d = %+v, %v; want %+v", want.NoteID, got, err, want)
			}
		}
	}
	check()
	// An indexed update must be visible immediately, without another listing or cached reference.
	notes[0].Mtime = 99
	notes[0].Body = "Changed #updated"
	notes[0].Meta = note.Metadata{Title: "Renamed", Tags: []string{"new"}, Flags: []string{"DEPRECATED"}}
	if err := v.store.UpsertNote(notes[0]); err != nil {
		t.Fatal(err)
	}
	check()
	got, err := v.noteByID(100)
	if err != nil || got.Title != "Renamed" || got.Mtime != 99 {
		t.Fatalf("updated lookup = %+v, %v", got, err)
	}
	if err := v.store.DeleteNote(200); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{200, 300, 999, 0, -1} {
		got, err := v.noteByID(id)
		if err == nil || err.Error() != fmt.Sprintf("note %d is not indexed", id) || !reflect.DeepEqual(got, store.SearchResult{}) {
			t.Fatalf("missing/excluded lookup %d = %+v, %v", id, got, err)
		}
	}
}

func TestNoteByIDPreservesStoreError(t *testing.T) {
	v := noteLookupView(t)
	if err := v.store.Close(); err != nil {
		t.Fatal(err)
	}
	_, want := v.store.SearchRefs()
	got, err := v.noteByID(100)
	if want == nil || err == nil || err.Error() != want.Error() || !reflect.DeepEqual(got, store.SearchResult{}) {
		t.Fatalf("closed-store lookup = %+v, %v; want error %v", got, err, want)
	}
}

func TestNoteByIDAllocationsDoNotScaleWithUnrelatedNotes(t *testing.T) {
	v := noteLookupView(t)
	add := func(id int64) {
		t.Helper()
		if err := v.store.UpsertNote(&note.Note{ID: id, Meta: note.Metadata{Title: fmt.Sprintf("Note %d", id), Tags: []string{"fixture"}}}); err != nil {
			t.Fatal(err)
		}
	}
	add(1)
	allocations := func() float64 {
		return testing.AllocsPerRun(10, func() {
			ref, err := v.noteByID(1)
			if err != nil || ref.NoteID != 1 {
				t.Fatalf("lookup = %+v, %v", ref, err)
			}
		})
	}
	small := allocations()
	for id := int64(2); id <= 1024; id++ {
		add(id)
	}
	large := allocations()
	t.Logf("lookup allocations: 1 note %.0f, 1024 notes %.0f", small, large)
	// Allow driver/runtime overhead, but reject materializing every unrelated note.
	// This bounds allocation growth, not elapsed time or the rest of the HTTP handler.
	if large > small*2 {
		t.Fatalf("lookup allocations grew with unrelated notes: %.0f -> %.0f", small, large)
	}
}

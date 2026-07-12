package store

import (
	"testing"

	"github.com/ttak0422/track/internal/track/note"
)

func TestEmbeddingRoundTrip(t *testing.T) {
	s := newTestStore(t)

	// An embedding references a note row (FK + cascade), so the note must exist first.
	if err := s.UpsertNote(&note.Note{ID: 1, Meta: note.Metadata{Title: "One"}}); err != nil {
		t.Fatalf("upsert note: %v", err)
	}
	if err := s.UpsertEmbedding(1, "hash-a", []float32{0.5, -1.5, 2}); err != nil {
		t.Fatalf("upsert embedding: %v", err)
	}

	hashes, err := s.EmbeddingHashes()
	if err != nil {
		t.Fatalf("hashes: %v", err)
	}
	if hashes[1] != "hash-a" {
		t.Fatalf("hash = %q, want hash-a", hashes[1])
	}

	all, err := s.AllEmbeddings()
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if len(all) != 1 || all[0].Title != "One" || len(all[0].Vector) != 3 || all[0].Vector[2] != 2 {
		t.Fatalf("unexpected embeddings: %+v", all)
	}

	// Upsert replaces the vector and hash in place.
	if err := s.UpsertEmbedding(1, "hash-b", []float32{9}); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	all, _ = s.AllEmbeddings()
	if len(all) != 1 || len(all[0].Vector) != 1 || all[0].Vector[0] != 9 {
		t.Fatalf("upsert did not replace: %+v", all)
	}

	// Deleting the note cascades to its embedding.
	if err := s.DeleteNote(1); err != nil {
		t.Fatalf("delete: %v", err)
	}
	all, _ = s.AllEmbeddings()
	if len(all) != 0 {
		t.Fatalf("embedding should cascade on note delete, got %+v", all)
	}
}

// TestAllEmbeddingsExcludesJournals guards the ADR 0037 rule that only kind == note ranks: a journal
// row that reaches the embeddings table anyway must never come back from AllEmbeddings.
func TestAllEmbeddingsExcludesJournals(t *testing.T) {
	s := newTestStore(t)

	if err := s.UpsertNote(&note.Note{ID: 1, Kind: "note", Meta: note.Metadata{Title: "One"}}); err != nil {
		t.Fatalf("upsert note: %v", err)
	}
	if err := s.UpsertNote(&note.Note{ID: 20260101, Kind: "journal", Meta: note.Metadata{Title: "20260101"}}); err != nil {
		t.Fatalf("upsert journal: %v", err)
	}
	if err := s.UpsertEmbedding(1, "h-note", []float32{1, 0}); err != nil {
		t.Fatalf("embed note: %v", err)
	}
	if err := s.UpsertEmbedding(20260101, "h-journal", []float32{0, 1}); err != nil {
		t.Fatalf("embed journal: %v", err)
	}

	all, err := s.AllEmbeddings()
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if len(all) != 1 || all[0].NoteID != 1 || all[0].FileKind != "note" {
		t.Fatalf("AllEmbeddings must return only the note-kind row, got %+v", all)
	}
}

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

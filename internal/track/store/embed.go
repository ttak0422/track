package store

import (
	"encoding/json"
)

// Embedding is one note's cached vector plus the display fields the similar command needs to rank and
// present it, so a nearest-neighbour scan reads everything from a single join.
type Embedding struct {
	NoteID   int64
	FileKind string
	Title    string
	Vector   []float32
}

// UpsertEmbedding stores (or replaces) the vector and content hash for a note. The vector is kept as a
// JSON float array — the same shape the embedder emits — so the cache is human-readable and portable;
// pack it into a float32 blob only if vector storage size ever matters.
func (s *Store) UpsertEmbedding(noteID int64, hash string, vector []float32) error {
	blob, err := json.Marshal(vector)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT INTO embeddings (note_id, hash, vector) VALUES (?, ?, ?)
		 ON CONFLICT(note_id) DO UPDATE SET hash=excluded.hash, vector=excluded.vector`,
		noteID, hash, string(blob),
	)
	return err
}

// EmbeddingHashes maps note id to the content hash its stored vector was computed from, so the caller can
// skip re-embedding notes whose text has not changed.
func (s *Store) EmbeddingHashes() (map[int64]string, error) {
	rows, err := s.db.Query(`SELECT note_id, hash FROM embeddings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var hash string
		if err := rows.Scan(&id, &hash); err != nil {
			return nil, err
		}
		out[id] = hash
	}
	return out, rows.Err()
}

// AllEmbeddings returns every cached vector joined with its note's kind and title, for a nearest-neighbour
// scan. Journals are excluded from the keyword surface elsewhere but kept here: similarity across any
// indexed note is meaningful.
func (s *Store) AllEmbeddings() ([]Embedding, error) {
	rows, err := s.db.Query(
		`SELECT e.note_id, n.kind, n.title, e.vector
		 FROM embeddings e JOIN notes n ON n.id = e.note_id`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Embedding
	for rows.Next() {
		var e Embedding
		var blob string
		if err := rows.Scan(&e.NoteID, &e.FileKind, &e.Title, &blob); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(blob), &e.Vector); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

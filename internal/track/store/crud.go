package store

import (
	"github.com/ttak0422/track/internal/track/note"
)

// Keyword is one entry in the auto-link dictionary: a note title that, written as [[title]], links to NoteID.
type Keyword struct {
	Term     string `json:"term"`
	NoteID   int64  `json:"note_id"`
	FileKind string `json:"file_kind"`
	Path     string `json:"path,omitempty"`
	Kind     string `json:"kind"`
}

// NoteRef is a lightweight reference to a note.
type NoteRef struct {
	NoteID   int64  `json:"note_id"`
	FileKind string `json:"file_kind"`
	Path     string `json:"path,omitempty"`
	Title    string `json:"title"`
}

// UpsertNote inserts or updates a note row and replaces its tags in a single transaction.
func (s *Store) UpsertNote(n *note.Note) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	kind := n.Kind
	if kind == "" {
		kind = "note"
	}
	if _, err := tx.Exec(
		`INSERT INTO notes (id, kind, title, created, mtime)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   kind=excluded.kind, title=excluded.title, created=excluded.created, mtime=excluded.mtime`,
		n.ID, kind, n.Meta.Title, n.Meta.Created, n.Mtime,
	); err != nil {
		return err
	}

	if _, err := tx.Exec(`DELETE FROM tags WHERE note_id = ?`, n.ID); err != nil {
		return err
	}
	for _, tg := range n.Meta.Tags {
		if tg == "" {
			continue
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO tags (note_id, tag) VALUES (?, ?)`, n.ID, tg); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(`DELETE FROM tasks WHERE note_id = ?`, n.ID); err != nil {
		return err
	}
	for _, t := range n.Tasks {
		if _, err := tx.Exec(
			`INSERT INTO tasks (note_id, line, state, done, priority, scheduled, due, completed, text)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			n.ID, t.Line, t.State, t.Done, t.Priority, t.Scheduled, t.Due, t.Completed, t.Text,
		); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(`DELETE FROM note_days WHERE note_id = ?`, n.ID); err != nil {
		return err
	}
	// note.ActivityDays owns which days a note surfaces on (journals none, created-day fallback), so the
	// index and the static-site export apply the same rule.
	for _, d := range note.ActivityDays(kind, n.Meta) {
		if d == "" {
			continue
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO note_days (note_id, day) VALUES (?, ?)`, n.ID, d); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// DeleteNote removes a note; tags and links cascade.
func (s *Store) DeleteNote(id int64) error {
	_, err := s.db.Exec(`DELETE FROM notes WHERE id = ?`, id)
	return err
}

// ReplaceLinks sets the outgoing links for srcID, ignoring self-links.
func (s *Store) ReplaceLinks(srcID int64, dstIDs []int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM links WHERE src_id = ?`, srcID); err != nil {
		return err
	}
	for _, dst := range dstIDs {
		if dst == srcID {
			continue
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO links (src_id, dst_id) VALUES (?, ?)`, srcID, dst); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Keywords returns the full auto-link dictionary (note titles).
func (s *Store) Keywords() ([]Keyword, error) {
	rows, err := s.db.Query(
		`SELECT k.term, k.note_id, n.kind, k.kind
		 FROM keywords k JOIN notes n ON n.id = k.note_id
		 WHERE n.kind IN ('note', 'journal')`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Keyword
	for rows.Next() {
		var k Keyword
		if err := rows.Scan(&k.Term, &k.NoteID, &k.FileKind, &k.Kind); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// ResolveTerm finds the note a title points to.
func (s *Store) ResolveTerm(term string) (NoteRef, bool, error) {
	var ref NoteRef
	err := s.db.QueryRow(
		`SELECT k.note_id, n.kind, n.title
		 FROM keywords k JOIN notes n ON n.id = k.note_id
		 WHERE k.term = ? AND n.kind IN ('note', 'journal') LIMIT 1`,
		term,
	).Scan(&ref.NoteID, &ref.FileKind, &ref.Title)
	if err != nil {
		if isNoRows(err) {
			return NoteRef{}, false, nil
		}
		return NoteRef{}, false, err
	}
	return ref, true, nil
}

// Backlinks returns notes that link to the given note id, most recently updated first — the one
// ordering every note-list surface shares (see sortRefs in webui and the static bundle).
func (s *Store) Backlinks(id int64) ([]NoteRef, error) {
	rows, err := s.db.Query(
		`SELECT n.id, n.kind, n.title
		 FROM links l JOIN notes n ON n.id = l.src_id
		 WHERE l.dst_id = ? ORDER BY n.mtime DESC, n.id`,
		id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNoteRefs(rows)
}

// NotesOnDay returns the notes active (created or updated) on the given local calendar day, most
// recently updated first — the shared note-list order, so a calendar cell's visible titles and the day
// page it opens read identically.
func (s *Store) NotesOnDay(day string) ([]NoteRef, error) {
	rows, err := s.db.Query(
		`SELECT n.id, n.kind, n.title
		 FROM note_days d JOIN notes n ON n.id = d.note_id
		 WHERE d.day = ? ORDER BY n.mtime DESC, n.id`,
		day,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNoteRefs(rows)
}

// AllNotes returns every note as a NoteRef, ordered by id.
func (s *Store) AllNotes() ([]NoteRef, error) {
	rows, err := s.db.Query(`SELECT id, kind, title FROM notes ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNoteRefs(rows)
}

// NoteMtimes maps note id to stored mtime, used by the indexer to detect changed and deleted files.
func (s *Store) NoteMtimes() (map[int64]int64, error) {
	rows, err := s.db.Query(`SELECT id, mtime FROM notes`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[int64]int64)
	for rows.Next() {
		var id, mtime int64
		if err := rows.Scan(&id, &mtime); err != nil {
			return nil, err
		}
		out[id] = mtime
	}
	return out, rows.Err()
}

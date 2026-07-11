package store

import "github.com/ttak0422/track/internal/track/note"

// Hierarchy queries over the conventional "up" relation property (note.UpProp): a note declares its
// parent with "up:: [[Parent]]", and these resolve that declaration through the same keyword
// dictionary [[links]] use, so a parent is whatever the link would navigate to.

// UpNotes returns the notes a note's "up" properties point at, in property order. Values that do not
// resolve to a note are skipped, mirroring how an unresolved [[link]] is not a graph edge.
func (s *Store) UpNotes(id int64) ([]NoteRef, error) {
	rows, err := s.db.Query(
		`SELECT k.note_id, n.kind, n.title
		 FROM props p
		 JOIN keywords k ON k.term = p.value
		 JOIN notes n ON n.id = k.note_id
		 WHERE p.note_id = ? AND p.key = ? AND p.type = ? AND k.note_id != ?
		   AND n.kind IN ('note', 'journal')
		 GROUP BY k.note_id ORDER BY min(p.ord)`,
		id, note.UpProp, note.TypeLink, id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNoteRefs(rows)
}

// ChildNotes returns the notes whose "up" property points at this note, in the shared note-list
// order (most recently updated first).
func (s *Store) ChildNotes(id int64) ([]NoteRef, error) {
	rows, err := s.db.Query(
		`SELECT n.id, n.kind, n.title
		 FROM props p
		 JOIN keywords k ON k.term = p.value AND k.note_id = ?
		 JOIN notes n ON n.id = p.note_id
		 WHERE p.key = ? AND p.type = ? AND n.id != ?
		   AND n.kind IN ('note', 'journal')
		 GROUP BY n.id ORDER BY n.mtime DESC, n.id`,
		id, note.UpProp, note.TypeLink, id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNoteRefs(rows)
}

// Trail returns the chain of "up" ancestors of a note, root first, the immediate parent last. A note
// with several parents follows the first one (property order), so the trail is a single path; a
// cycle stops the walk where it would revisit a note.
func (s *Store) Trail(id int64) ([]NoteRef, error) {
	var trail []NoteRef
	seen := map[int64]bool{id: true}
	cur := id
	for {
		parents, err := s.UpNotes(cur)
		if err != nil {
			return nil, err
		}
		if len(parents) == 0 || seen[parents[0].NoteID] {
			return trail, nil
		}
		p := parents[0]
		seen[p.NoteID] = true
		trail = append([]NoteRef{p}, trail...)
		cur = p.NoteID
	}
}

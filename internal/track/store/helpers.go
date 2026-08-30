package store

import (
	"database/sql"
	"errors"
)

func isNoRows(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

// scanNoteRefs scans the four-column shape every note-list query returns — id, kind, title, and the
// note's char(31)-joined flags — so a NoteRef carries the same flags SearchResult does. The flags
// column is the 4th; splitTags splits it on char(31) like search rows.
func scanNoteRefs(rows *sql.Rows) ([]NoteRef, error) {
	var out []NoteRef
	for rows.Next() {
		var r NoteRef
		var flags string
		if err := rows.Scan(&r.NoteID, &r.FileKind, &r.Title, &flags); err != nil {
			return nil, err
		}
		r.Flags = splitTags(flags)
		out = append(out, r)
	}
	return out, rows.Err()
}

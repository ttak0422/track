package store

import (
	"database/sql"
	"errors"
)

func isNoRows(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

func scanNoteRefs(rows *sql.Rows) ([]NoteRef, error) {
	var out []NoteRef
	for rows.Next() {
		var r NoteRef
		if err := rows.Scan(&r.NoteID, &r.FileKind, &r.Title); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

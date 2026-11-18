package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var ErrJournalLookupAmbiguous = errors.New("more than one journal has the requested exact title")

type JournalLookupNote struct {
	NoteID   int64  `json:"note_id"`
	FileKind string `json:"file_kind"`
	Title    string `json:"title"`
}

// ValidJournalLookupTitle accepts only real monthly/daily ASCII titles, not link targets or IDs.
func ValidJournalLookupTitle(title string) bool {
	layout := "20060102"
	if len(title) == 6 {
		layout = "200601"
	} else if len(title) != 8 {
		return false
	}
	for _, c := range title {
		if c < '0' || c > '9' {
			return false
		}
	}
	date, err := time.Parse(layout, title)
	return err == nil && date.Year() >= 1 && date.Year() <= 9998 && date.Format(layout) == title
}

// LookupJournalContext performs an exact kind/title lookup in an already-open verified snapshot.
// Two matches are enough to reject ambiguity; the context bounds scans without a title index.
func (s *Store) LookupJournalContext(ctx context.Context, title string) (*JournalLookupNote, int64, error) {
	if !ValidJournalLookupTitle(title) {
		return nil, 0, errors.New("invalid journal lookup title")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	revision, err := calendarIndexHealth(ctx, tx)
	if err != nil {
		return nil, 0, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,kind,title FROM notes WHERE kind='journal' AND title=? LIMIT 2`, title)
	if err != nil {
		return nil, 0, err
	}
	var found *JournalLookupNote
	for rows.Next() {
		var n JournalLookupNote
		if err = rows.Scan(&n.NoteID, &n.FileKind, &n.Title); err != nil {
			break
		}
		if n.NoteID <= 0 || n.FileKind != "journal" || n.Title != title {
			err = errors.New("invalid indexed journal identity")
			break
		}
		if found != nil {
			err = ErrJournalLookupAmbiguous
			break
		}
		found = &n
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	if err = tx.Commit(); err != nil {
		return nil, 0, err
	}
	if err = s.CalendarActivityRevisionContext(ctx, revision); err != nil {
		return nil, 0, err
	}
	return found, revision, nil
}

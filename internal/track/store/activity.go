package store

// ActivityDay is a day bucket for note activity in the local timezone.
type ActivityDay struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

// AllNoteDays maps each note id to its activity days ("YYYY-MM-DD", ascending). Journals carry no
// note_days rows, so they are naturally absent. The web calendar derives its per-day note lists from
// this via the notes listing.
func (s *Store) AllNoteDays() (map[int64][]string, error) {
	rows, err := s.db.Query(`SELECT note_id, day FROM note_days ORDER BY day`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[int64][]string)
	for rows.Next() {
		var id int64
		var day string
		if err := rows.Scan(&id, &day); err != nil {
			return nil, err
		}
		out[id] = append(out[id], day)
	}
	return out, rows.Err()
}

// NoteActivityRange returns the number of notes active on each day within [since, until] (inclusive),
// counted from note_days. Journals are excluded upstream when note_days is populated, so the counts
// reflect real notes worked on. Only days that have activity are returned, ascending by day. since and
// until are local calendar-day strings ("YYYY-MM-DD"), matching how note_days stores days.
func (s *Store) NoteActivityRange(since, until string) ([]ActivityDay, error) {
	rows, err := s.db.Query(
		`SELECT day, COUNT(*) AS n
		 FROM note_days
		 WHERE day >= ? AND day <= ?
		 GROUP BY day
		 ORDER BY day`,
		since, until,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]ActivityDay, 0)
	for rows.Next() {
		var d ActivityDay
		if err := rows.Scan(&d.Date, &d.Count); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

package store

// ActivityDay is a day bucket for note activity in the local timezone.
type ActivityDay struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
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

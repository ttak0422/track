package store

import (
	"slices"
	"time"
)

// ActivityDay is a day bucket for note updates in the local timezone.
type ActivityDay struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

// ActivitySince returns note update counts grouped by local calendar day.
func (s *Store) ActivitySince(start time.Time) ([]ActivityDay, error) {
	start = start.In(time.Local)
	rows, err := s.db.Query(
		`SELECT mtime
		 FROM notes
		 WHERE kind IN ('note', 'journal') AND mtime >= ?
		 ORDER BY mtime`,
		start.Unix(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := map[string]int{}
	for rows.Next() {
		var mtime int64
		if err := rows.Scan(&mtime); err != nil {
			return nil, err
		}
		day := time.Unix(mtime, 0).In(time.Local).Format("2006-01-02")
		counts[day]++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]ActivityDay, 0, len(counts))
	for day, count := range counts {
		out = append(out, ActivityDay{Date: day, Count: count})
	}
	slices.SortFunc(out, func(a, b ActivityDay) int {
		switch {
		case a.Date < b.Date:
			return -1
		case a.Date > b.Date:
			return 1
		default:
			return 0
		}
	})
	return out, nil
}

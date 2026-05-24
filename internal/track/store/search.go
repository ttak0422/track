package store

import (
	"strings"
	"unicode/utf8"
)

// SearchResult is one hit from a content/title/alias search.
type SearchResult struct {
	NoteID  int64  `json:"note_id"`
	Path    string `json:"path"`
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
}

// Search returns notes whose title, body, or any alias contains query
// (case-insensitive substring). A short snippet is built around the first body
// match. FTS5 can replace this later behind the same signature.
func (s *Store) Search(query string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 50
	}
	like := "%" + query + "%"
	rows, err := s.db.Query(
		`SELECT DISTINCT n.id, n.path, n.title, n.body
		 FROM notes n
		 LEFT JOIN aliases a ON a.note_id = n.id
		 WHERE n.title LIKE ? OR n.body LIKE ? OR a.alias LIKE ?
		 ORDER BY n.id LIMIT ?`,
		like, like, like, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SearchResult
	for rows.Next() {
		var r SearchResult
		var body string
		if err := rows.Scan(&r.NoteID, &r.Path, &r.Title, &body); err != nil {
			return nil, err
		}
		r.Snippet = snippet(body, query)
		out = append(out, r)
	}
	return out, rows.Err()
}

// snippet returns a short, single-line excerpt of body centered on the first
// case-insensitive occurrence of query.
func snippet(body, query string) string {
	const width = 80
	flat := strings.Join(strings.Fields(body), " ")
	if flat == "" {
		return ""
	}
	idx := strings.Index(strings.ToLower(flat), strings.ToLower(query))
	if idx < 0 {
		return truncate(flat, width)
	}
	start := backToRune(flat, max(idx-width/2, 0))
	excerpt := truncate(flat[start:], width)
	if start > 0 {
		excerpt = "…" + excerpt
	}
	return excerpt
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	end := max
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end] + "…"
}

func backToRune(s string, i int) int {
	for i > 0 && !utf8.RuneStart(s[i]) {
		i--
	}
	return i
}

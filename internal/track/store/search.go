package store

import (
	"fmt"
)

type SearchScope string

const (
	SearchAll   SearchScope = "all"
	SearchTitle SearchScope = "title"
	SearchBody  SearchScope = "body"
)

// SearchResult is one hit from a title/alias search, or a file-backed body search assembled by callers.
// Line and Snippet locate the first matching body line (1-based); they are zero/empty
// when the hit is title-only.
type SearchResult struct {
	NoteID  int64  `json:"note_id"`
	Path    string `json:"path"`
	Title   string `json:"title"`
	Line    int    `json:"line,omitempty"`
	Snippet string `json:"snippet,omitempty"`
}

// Search returns notes whose title or any alias contains query (case-insensitive substring).
// FTS5 can replace this later behind the same signature.
func (s *Store) Search(query string, limit int) ([]SearchResult, error) {
	return s.SearchScoped(query, limit, SearchAll)
}

func (s *Store) SearchScoped(query string, limit int, scope SearchScope) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 50
	}
	like := "%" + query + "%"
	sql, args, err := searchQuery(scope, like, limit)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.NoteID, &r.Title); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func searchQuery(scope SearchScope, like string, limit int) (string, []any, error) {
	switch scope {
	case SearchAll:
		return `SELECT DISTINCT n.id, n.title
		 FROM notes n
		 LEFT JOIN aliases a ON a.note_id = n.id
		 WHERE n.title LIKE ? OR a.alias LIKE ?
		 ORDER BY n.id LIMIT ?`, []any{like, like, limit}, nil
	case SearchTitle:
		return `SELECT n.id, n.title
		 FROM notes n
		 WHERE n.title LIKE ?
		 ORDER BY n.id LIMIT ?`, []any{like, limit}, nil
	case SearchBody:
		return "", nil, fmt.Errorf("body search is not stored in the SQLite cache")
	default:
		return "", nil, fmt.Errorf("unknown search scope %q", scope)
	}
}

package store

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

type SearchScope string

const (
	SearchAll   SearchScope = "all"
	SearchTitle SearchScope = "title"
	SearchBody  SearchScope = "body"
)

// SearchResult is one hit from a content/title/alias search.
// Line and Snippet locate the first matching body line (1-based); they are zero/empty
// when the hit is title-only or the scope is title.
type SearchResult struct {
	NoteID  int64  `json:"note_id"`
	Path    string `json:"path"`
	Title   string `json:"title"`
	Line    int    `json:"line,omitempty"`
	Snippet string `json:"snippet,omitempty"`
}

// Search returns notes whose title, body, or any alias contains query (case-insensitive substring).
// Each hit carries the first matching body line via Line/Snippet.
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
		var body string
		if err := rows.Scan(&r.NoteID, &r.Path, &r.Title, &body); err != nil {
			return nil, err
		}
		// Title-scoped searches never touch the body, so leave Line/Snippet empty.
		if scope != SearchTitle {
			r.Line, r.Snippet = lineMatch(body, query)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func searchQuery(scope SearchScope, like string, limit int) (string, []any, error) {
	switch scope {
	case SearchAll:
		return `SELECT DISTINCT n.id, n.path, n.title, n.body
		 FROM notes n
		 LEFT JOIN aliases a ON a.note_id = n.id
		 WHERE n.title LIKE ? OR n.body LIKE ? OR a.alias LIKE ?
		 ORDER BY n.id LIMIT ?`, []any{like, like, like, limit}, nil
	case SearchTitle:
		return `SELECT n.id, n.path, n.title, n.body
		 FROM notes n
		 WHERE n.title LIKE ?
		 ORDER BY n.id LIMIT ?`, []any{like, limit}, nil
	case SearchBody:
		return `SELECT n.id, n.path, n.title, n.body
		 FROM notes n
		 WHERE n.body LIKE ?
		 ORDER BY n.id LIMIT ?`, []any{like, limit}, nil
	default:
		return "", nil, fmt.Errorf("unknown search scope %q", scope)
	}
}

// lineMatch returns the 1-based number and trimmed text of the first body line that
// contains query (case-insensitive). It returns (0, "") when no line matches, so a
// title-only hit in an "all" search carries no body location.
func lineMatch(body, query string) (int, string) {
	lq := strings.ToLower(query)
	for i, line := range strings.Split(body, "\n") {
		if strings.Contains(strings.ToLower(line), lq) {
			return i + 1, truncate(strings.TrimSpace(line), 120)
		}
	}
	return 0, ""
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

package store

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ttak0422/track/internal/track/note"
)

type SearchScope string

const (
	SearchAll   SearchScope = "all"
	SearchTitle SearchScope = "title"
	SearchBody  SearchScope = "body"
)

// SearchResult is one hit from a title search, or a file-backed body search assembled by callers.
// Line and Snippet locate the first matching body line (1-based); they are zero/empty
// when the hit is title-only.
type SearchResult struct {
	NoteID        int64    `json:"note_id"`
	FileKind      string   `json:"file_kind"`
	Path          string   `json:"path"`
	Title         string   `json:"title"`
	Tags          []string `json:"tags,omitempty"`
	GeneratedByAI bool     `json:"generated_by_ai,omitempty"`
	Line          int      `json:"line,omitempty"`
	Snippet       string   `json:"snippet,omitempty"`
	Mtime         int64    `json:"-"`
}

// Search returns notes whose title contains query (case-insensitive substring).
// FTS5 can replace this later behind the same signature.
func (s *Store) Search(query string, limit int) ([]SearchResult, error) {
	return s.SearchScoped(query, limit, SearchAll)
}

func (s *Store) SearchScoped(query string, limit int, scope SearchScope) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 50
	}
	sql, args, err := searchQuery(scope, query, limit)
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
		var generated int
		var tags string
		if err := rows.Scan(&r.NoteID, &r.FileKind, &r.Title, &r.Mtime, &tags, &generated); err != nil {
			return nil, err
		}
		r.Tags = splitTags(tags)
		r.GeneratedByAI = generated != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

func searchQuery(scope SearchScope, query string, limit int) (string, []any, error) {
	like := "%" + query + "%"
	prefix := query + "%"
	switch scope {
	case SearchAll:
		return `SELECT n.id, n.kind, n.title, n.mtime,
		   COALESCE((
		     SELECT group_concat(tag, char(31))
		     FROM (SELECT tag FROM tags WHERE note_id = n.id ORDER BY tag)
		   ), '') AS tags,
		   CASE WHEN EXISTS (
		     SELECT 1 FROM tags t WHERE t.note_id = n.id AND t.tag = ?
		   ) THEN 1 ELSE 0 END AS generated_by_ai
		 FROM notes n
		 WHERE n.kind IN ('note', 'journal') AND n.title LIKE ?
		 ORDER BY
		   CASE WHEN n.title = ? COLLATE NOCASE THEN 0 ELSE 1 END,
		   CASE WHEN n.title LIKE ? THEN 0 ELSE 1 END,
		   n.mtime DESC,
		   generated_by_ai ASC,
		   n.id DESC
		 LIMIT ?`, []any{note.GeneratedByAITag, like, query, prefix, limit}, nil
	case SearchTitle:
		return `SELECT n.id, n.kind, n.title, n.mtime,
		   COALESCE((
		     SELECT group_concat(tag, char(31))
		     FROM (SELECT tag FROM tags WHERE note_id = n.id ORDER BY tag)
		   ), '') AS tags,
		   CASE WHEN EXISTS (
		     SELECT 1 FROM tags t WHERE t.note_id = n.id AND t.tag = ?
		   ) THEN 1 ELSE 0 END AS generated_by_ai
		 FROM notes n
		 WHERE n.kind IN ('note', 'journal') AND n.title LIKE ?
		 ORDER BY
		   CASE WHEN n.title = ? COLLATE NOCASE THEN 0 ELSE 1 END,
		   CASE WHEN n.title LIKE ? THEN 0 ELSE 1 END,
		   n.mtime DESC,
		   generated_by_ai ASC,
		   n.id DESC
		 LIMIT ?`, []any{note.GeneratedByAITag, like, query, prefix, limit}, nil
	case SearchBody:
		return "", nil, fmt.Errorf("body search is not stored in the SQLite cache")
	default:
		return "", nil, fmt.Errorf("unknown search scope %q", scope)
	}
}

// SearchRefs returns indexed notes with search-only ranking/display metadata.
func (s *Store) SearchRefs() ([]SearchResult, error) {
	rows, err := s.db.Query(
		`SELECT n.id, n.kind, n.title, n.mtime,
		   COALESCE((
		     SELECT group_concat(tag, char(31))
		     FROM (SELECT tag FROM tags WHERE note_id = n.id ORDER BY tag)
		   ), '') AS tags,
		   CASE WHEN EXISTS (
		     SELECT 1 FROM tags t WHERE t.note_id = n.id AND t.tag = ?
		   ) THEN 1 ELSE 0 END AS generated_by_ai
		 FROM notes n
		 WHERE n.kind IN ('note', 'journal')`,
		note.GeneratedByAITag,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SearchResult
	for rows.Next() {
		var r SearchResult
		var generated int
		var tags string
		if err := rows.Scan(&r.NoteID, &r.FileKind, &r.Title, &r.Mtime, &tags, &generated); err != nil {
			return nil, err
		}
		r.Tags = splitTags(tags)
		r.GeneratedByAI = generated != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

func splitTags(value string) []string {
	if value == "" {
		return nil
	}
	tags := strings.Split(value, "\x1f")
	tags = slices.DeleteFunc(tags, func(tag string) bool { return tag == "" })
	if len(tags) == 0 {
		return nil
	}
	return tags
}

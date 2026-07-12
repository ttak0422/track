package store

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// minTrigram is the shortest term the FTS5 trigram tokenizer can match: a term of one or two
// characters forms no trigram, so a body query containing such a term must fall back to a scan.
const minTrigram = 3

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
	NoteID   int64    `json:"note_id"`
	FileKind string   `json:"file_kind"`
	Path     string   `json:"path"`
	Title    string   `json:"title"`
	Tags     []string `json:"tags,omitempty"`
	Days     []string `json:"days,omitempty"` // activity days (YYYY-MM-DD); only the notes listing fills this
	Line     int      `json:"line,omitempty"`
	Snippet  string   `json:"snippet,omitempty"`
	Mtime    int64    `json:"-"`
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
		var tags string
		if err := rows.Scan(&r.NoteID, &r.FileKind, &r.Title, &r.Mtime, &tags); err != nil {
			return nil, err
		}
		r.Tags = splitTags(tags)
		out = append(out, r)
	}
	return out, rows.Err()
}

func searchQuery(scope SearchScope, query string, limit int) (string, []any, error) {
	switch scope {
	case SearchAll, SearchTitle:
		// title search runs against the SQLite cache; body search does not (see below).
	case SearchBody:
		// Body search runs through the FTS5 index, not this title query — see SearchBodyFTS.
		return "", nil, fmt.Errorf("use SearchBodyFTS for body scope")
	default:
		return "", nil, fmt.Errorf("unknown search scope %q", scope)
	}

	if parsed, ok := parseTaggedQuery(query); ok {
		sql, args := searchTagged(parsed, limit)
		return sql, args, nil
	}

	titleClause, titleArgs := titleMatchClause(query)
	where := "n.kind IN ('note', 'journal')"
	if titleClause != "" {
		where += " AND (" + titleClause + ")"
	}
	sql := `SELECT n.id, n.kind, n.title, n.mtime,
	   COALESCE((
	     SELECT group_concat(tag, char(31))
	     FROM (SELECT tag FROM tags WHERE note_id = n.id ORDER BY tag)
	   ), '') AS tags
	 FROM notes n
	 WHERE ` + where + `
	 ORDER BY
	   CASE WHEN n.title = ? COLLATE NOCASE THEN 0 ELSE 1 END,
	   CASE WHEN n.title LIKE ? THEN 0 ELSE 1 END,
	   n.mtime DESC,
	   n.id DESC
	 LIMIT ?`
	args := append(titleArgs, query, query+"%", limit)
	return sql, args, nil
}

// titleMatchClause builds a WHERE fragment matching n.title against a text query that supports
// space-separated implicit-AND terms with an uppercase OR between alternative groups. It returns
// ("", nil) for an empty query, so the caller matches every title. Example: "a b OR c" yields
// "(n.title LIKE ? AND n.title LIKE ?) OR (n.title LIKE ?)" with args ["%a%", "%b%", "%c%"].
func titleMatchClause(text string) (string, []any) {
	groups := splitOrGroups(text)
	var ors []string
	var args []any
	for _, terms := range groups {
		var ands []string
		for _, term := range terms {
			ands = append(ands, "n.title LIKE ?")
			args = append(args, "%"+term+"%")
		}
		ors = append(ors, "("+strings.Join(ands, " AND ")+")")
	}
	if len(ors) == 0 {
		return "", nil
	}
	return strings.Join(ors, " OR "), args
}

// splitOrGroups splits a query into OR-separated groups of AND terms. Uppercase OR ends a group and
// uppercase AND is the (implicit) default and is dropped, so a bare lowercase "and"/"or" stays a
// literal search term.
func splitOrGroups(text string) [][]string {
	var groups [][]string
	var cur []string
	flush := func() {
		if len(cur) > 0 {
			groups = append(groups, cur)
			cur = nil
		}
	}
	for _, field := range strings.Fields(text) {
		switch field {
		case "OR":
			flush()
		case "AND":
			// implicit between terms; nothing to add
		default:
			cur = append(cur, field)
		}
	}
	flush()
	return groups
}

type parsedTaggedQuery struct {
	Text string
	Tags []string
}

func parseTaggedQuery(query string) (parsedTaggedQuery, bool) {
	var parsed parsedTaggedQuery
	var text []string
	seen := map[string]bool{}
	for _, field := range strings.Fields(query) {
		if strings.HasPrefix(field, "#") {
			tag := strings.TrimSpace(strings.TrimPrefix(field, "#"))
			if tag == "" || seen[tag] {
				continue
			}
			seen[tag] = true
			parsed.Tags = append(parsed.Tags, tag)
			continue
		}
		text = append(text, field)
	}
	parsed.Text = strings.Join(text, " ")
	return parsed, len(parsed.Tags) > 0
}

// searchTagged builds the SQL and args for a query that carries one or more #tags, combining the tag
// filters (AND) with the same AND/OR title matching used for a plain query.
func searchTagged(parsed parsedTaggedQuery, limit int) (string, []any) {
	where := []string{"n.kind IN ('note', 'journal')"}
	var whereArgs []any
	for _, tag := range parsed.Tags {
		where = append(where, "EXISTS (SELECT 1 FROM tags t WHERE t.note_id = n.id AND t.tag LIKE ?)")
		whereArgs = append(whereArgs, "%"+tag+"%")
	}
	if titleClause, titleArgs := titleMatchClause(parsed.Text); titleClause != "" {
		where = append(where, "("+titleClause+")")
		whereArgs = append(whereArgs, titleArgs...)
	}

	var order []string
	var orderArgs []any
	for _, tag := range parsed.Tags {
		order = append(order, `CASE WHEN EXISTS (
	     SELECT 1 FROM tags t WHERE t.note_id = n.id AND t.tag = ? COLLATE NOCASE
	   ) THEN 0 ELSE 1 END`)
		orderArgs = append(orderArgs, tag)
	}
	for _, tag := range parsed.Tags {
		order = append(order, `CASE WHEN EXISTS (
	     SELECT 1 FROM tags t WHERE t.note_id = n.id AND t.tag LIKE ?
	   ) THEN 0 ELSE 1 END`)
		orderArgs = append(orderArgs, tag+"%")
	}
	if parsed.Text != "" {
		order = append(order,
			"CASE WHEN n.title = ? COLLATE NOCASE THEN 0 ELSE 1 END",
			"CASE WHEN n.title LIKE ? THEN 0 ELSE 1 END",
		)
		orderArgs = append(orderArgs, parsed.Text, parsed.Text+"%")
	}
	order = append(order,
		"n.mtime DESC",
		"n.id DESC",
	)

	sql := `SELECT n.id, n.kind, n.title, n.mtime,
	   COALESCE((
	     SELECT group_concat(tag, char(31))
	     FROM (SELECT tag FROM tags WHERE note_id = n.id ORDER BY tag)
	   ), '') AS tags
	 FROM notes n
	 WHERE ` + strings.Join(where, " AND ") + `
	 ORDER BY ` + strings.Join(order, ",\n	   ") + `
	 LIMIT ?`
	args := append(whereArgs, orderArgs...)
	args = append(args, limit)
	return sql, args
}

// BodyGroups parses a body query into OR-separated groups of AND terms — the same grammar as title
// search: an uppercase OR separates alternatives, an uppercase AND is the implicit default between
// terms. A note matches when any one group's terms are all present. "a b OR c" yields [[a b] [c]].
func BodyGroups(query string) [][]string {
	return splitOrGroups(query)
}

// BodyQueryUsesFTS reports whether a body query can be served by the trigram FTS index: it needs at
// least one term and every term (across all OR groups) must be long enough to form a trigram. Shorter
// terms (a two-letter word or a two-character CJK word like 世界) have no trigram and are left to the
// per-file fallback.
func BodyQueryUsesFTS(query string) bool {
	groups := BodyGroups(query)
	if len(groups) == 0 {
		return false
	}
	for _, terms := range groups {
		for _, term := range terms {
			if utf8.RuneCountInString(term) < minTrigram {
				return false
			}
		}
	}
	return true
}

// SearchBodyFTS returns notes whose body matches the query's OR-groups (a note matches when any one
// group's terms are all present), ranked by FTS5 bm25 relevance then recency. Caller must ensure
// BodyQueryUsesFTS(query); a short-term query is handled by a scan. Results carry note metadata but no
// Line/Snippet — the caller locates those in the file, preserving the line-number contract while FTS
// does the matching and ranking.
func (s *Store) SearchBodyFTS(query string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 50
	}
	groups := BodyGroups(query)
	if len(groups) == 0 {
		return nil, nil
	}
	rows, err := s.db.Query(
		`SELECT n.id, n.kind, n.title, n.mtime,
		   COALESCE((
		     SELECT group_concat(tag, char(31))
		     FROM (SELECT tag FROM tags WHERE note_id = n.id ORDER BY tag)
		   ), '') AS tags
		 FROM notes_fts f
		 JOIN notes n ON n.id = f.rowid
		 WHERE notes_fts MATCH ? AND n.kind IN ('note', 'journal')
		 ORDER BY bm25(notes_fts), n.mtime DESC, n.id DESC
		 LIMIT ?`,
		ftsMatchExprGroups(groups), limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SearchResult
	for rows.Next() {
		var r SearchResult
		var tags string
		if err := rows.Scan(&r.NoteID, &r.FileKind, &r.Title, &r.Mtime, &tags); err != nil {
			return nil, err
		}
		r.Tags = splitTags(tags)
		out = append(out, r)
	}
	return out, rows.Err()
}

// ftsMatchExprGroups builds an FTS5 MATCH expression from OR-separated groups of AND terms:
// ("a" AND "b") OR ("c"). Each term is wrapped as a quoted string (doubling any embedded quote) so
// user punctuation is never parsed as FTS5 query syntax; terms within a group join with AND and the
// groups join with OR.
func ftsMatchExprGroups(groups [][]string) string {
	ors := make([]string, 0, len(groups))
	for _, terms := range groups {
		quoted := make([]string, len(terms))
		for i, term := range terms {
			quoted[i] = `"` + strings.ReplaceAll(term, `"`, `""`) + `"`
		}
		ors = append(ors, "("+strings.Join(quoted, " AND ")+")")
	}
	return strings.Join(ors, " OR ")
}

// SearchRefs returns indexed notes with search-only ranking/display metadata.
func (s *Store) SearchRefs() ([]SearchResult, error) {
	rows, err := s.db.Query(
		`SELECT n.id, n.kind, n.title, n.mtime,
		   COALESCE((
		     SELECT group_concat(tag, char(31))
		     FROM (SELECT tag FROM tags WHERE note_id = n.id ORDER BY tag)
		   ), '') AS tags
		 FROM notes n
		 WHERE n.kind IN ('note', 'journal')`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SearchResult
	for rows.Next() {
		var r SearchResult
		var tags string
		if err := rows.Scan(&r.NoteID, &r.FileKind, &r.Title, &r.Mtime, &tags); err != nil {
			return nil, err
		}
		r.Tags = splitTags(tags)
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

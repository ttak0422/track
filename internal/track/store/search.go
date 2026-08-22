package store

import (
	"cmp"
	// Aliased: this file builds queries in locals named sql, which would shadow the package.
	gosql "database/sql"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// minTrigram is the shortest term the FTS5 trigram tokenizer can match: a term of one or two
// characters forms no trigram, so a body query containing such a term must fall back to a scan.
const minTrigram = 3

// The columns every search row is scanned from (see scanSearchRows), named once so a second query
// cannot drift from the first.
const searchColumns = `n.id, n.kind, n.title, n.mtime, n.icon, n.seen_at, n.read_at,
	   COALESCE((
	     SELECT group_concat(tag, char(31))
	     FROM (SELECT tag FROM tags WHERE note_id = n.id ORDER BY tag)
	   ), '') AS tags`

type SearchScope string

const (
	SearchAll   SearchScope = "all"
	SearchTitle SearchScope = "title"
	SearchBody  SearchScope = "body"
	// SearchPath finds a note by the file it is stored in. Every kind's file name is derived from the
	// note's id (config.PathForKind), so this scope is an id lookup wearing a path's clothes — see
	// search.NoteIDFromFileName for what counts as naming a file.
	SearchPath SearchScope = "path"
)

// SearchResult is one hit from a title search, or a file-backed body search assembled by callers.
// Line and Snippet locate the first matching body line (1-based); they are zero/empty
// when the hit is title-only.
type SearchResult struct {
	NoteID   int64  `json:"note_id"`
	FileKind string `json:"file_kind"`
	// Vault is the registry name of the vault this hit came from, filled only by federated
	// cross-vault search ("" both for single-vault search and for the unregistered active vault).
	// It is the vault half of the (vault, id) identity a result needs once ids can repeat across
	// vaults; follow-up commands target the hit with --vault <name>.
	Vault string   `json:"vault,omitempty"`
	Path  string   `json:"path"`
	Title string   `json:"title"`
	Tags  []string `json:"tags,omitempty"`
	Days  []string `json:"days,omitempty"` // activity days (YYYY-MM-DD); only the notes listing fills this
	// Icon is the note's icon shown beside its title. The store fills it with the per-note sidecar
	// override; the serving layer (webui addSearchPaths / the static export) resolves it against the
	// config tag/kind mapping via config.NoteIcon, so an empty override falls back to the mapping.
	Icon    string `json:"icon,omitempty"`
	Line    int    `json:"line,omitempty"`
	Snippet string `json:"snippet,omitempty"`
	// Match says which search produced this hit, "title" or "body", so a frontend can group them.
	// It is not derivable from Line/Snippet: a body hit whose terms straddle lines legitimately has
	// neither (see the composition in internal/track/search).
	Match string `json:"match,omitempty"`
	// SeenAt / ReadAt are the note's shared reading milestones as unix seconds (0 = never): when any
	// device first opened the note in the web workspace and when viewing time there first crossed its
	// read threshold. They ride on every listing so NEW/read badges come from vault metadata that
	// syncs, not from per-browser localStorage (ADR 0072). The static export strips them: a public
	// site's badges stay per-visitor.
	SeenAt int64 `json:"seen_at,omitempty"`
	ReadAt int64 `json:"read_at,omitempty"`
	Mtime  int64 `json:"-"`
	// Rank is the sort key the vault's own query gave this hit: the packed title/tag rank vector for a
	// title search, bm25 for a body search, 0 for an unranked listing. It exists so results from
	// several vaults can be merged (MergeSearchResults) on exactly the key SQLite ranked them by —
	// recomputing it in Go would diverge from SQLite's LIKE and COLLATE NOCASE on non-ASCII titles and
	// on a query containing % or _. It never goes on the wire.
	Rank float64 `json:"-"`
}

// MergeSearchResults merges per-vault result pages into the global top-k. Every vault ran the same
// query under the same total order and each page is already that vault's top-k, so any row that
// places in the global first `limit` is in one of the pages: concatenating and re-applying the order
// gives exactly what one query spanning every vault would have returned. Ranks come from each vault's
// own SQL (SearchResult.Rank); bm25 scores from different FTS indexes are only approximately
// comparable, the same accepted imprecision cross-vault body search has always had.
func MergeSearchResults(pages [][]SearchResult, limit int) []SearchResult {
	total := 0
	for _, page := range pages {
		total += len(page)
	}
	out := make([]SearchResult, 0, total) // never nil: callers put this straight on the wire
	for _, page := range pages {
		out = append(out, page...)
	}
	slices.SortFunc(out, compareSearchResults)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// compareSearchResults is the ORDER BY of the single-vault queries expressed in Go: rank, then most
// recently modified, then highest id. The trailing vault comparison is the cross-vault tiebreak that
// keeps a merge deterministic when everything else collides — ids are vault-local, so two vaults can
// legitimately hold the same one (journal ids collide by construction).
func compareSearchResults(a, b SearchResult) int {
	if a.Rank != b.Rank {
		return cmp.Compare(a.Rank, b.Rank)
	}
	if a.Mtime != b.Mtime {
		return cmp.Compare(b.Mtime, a.Mtime)
	}
	if a.NoteID != b.NoteID {
		return cmp.Compare(b.NoteID, a.NoteID)
	}
	return cmp.Compare(a.Vault, b.Vault)
}

// rankExpr packs a rank vector of 0/1 CASE expressions — the ORDER BY of a title/tag search, most
// significant term first — into one integer column. Selecting the key instead of only ordering by it
// keeps SQL the single source of ranking semantics while letting a caller merge several vaults'
// results on it.
func rankExpr(cases []string) string {
	if len(cases) == 0 {
		return "0"
	}
	// One bit per term, so the key stays exact in the float64 it is scanned into up to 53 terms. Past
	// that the surplus terms only break ties far down the ranking, so drop them rather than lose the
	// key to rounding — it takes 52 #tags in one query to get there.
	if len(cases) > 53 {
		cases = cases[:53]
	}
	parts := make([]string, len(cases))
	for i, expr := range cases {
		parts[i] = fmt.Sprintf("(%s) * %d", expr, int64(1)<<(len(cases)-1-i))
	}
	return strings.Join(parts, " + ")
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
	return scanSearchRows(rows)
}

// SearchByID returns the one note stored at a given id, as a search result. It is the second half of
// the file-name lookup: a note's file is named from its id, so naming the file is naming the id.
func (s *Store) SearchByID(id int64) ([]SearchResult, error) {
	rows, err := s.db.Query(`SELECT `+searchColumns+`, 0 AS rank_key
	 FROM notes n
	 WHERE n.id = ? AND n.kind IN ('note', 'journal')
	 LIMIT 1`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSearchRows(rows)
}

func scanSearchRows(rows *gosql.Rows) ([]SearchResult, error) {
	var out []SearchResult
	for rows.Next() {
		var r SearchResult
		var tags string
		if err := rows.Scan(&r.NoteID, &r.FileKind, &r.Title, &r.Mtime, &r.Icon, &r.SeenAt, &r.ReadAt, &tags, &r.Rank); err != nil {
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
	case SearchPath:
		// A file name resolves to one id rather than a text match — see SearchByID.
		return "", nil, fmt.Errorf("use SearchByID for path scope")
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
	// The rank vector is selected as one packed column and ordered by that alias, so a caller merging
	// several vaults sorts on the very key SQLite ranked with. Its args come before the WHERE args.
	rank := rankExpr([]string{
		"CASE WHEN n.title = ? COLLATE NOCASE THEN 0 ELSE 1 END",
		"CASE WHEN n.title LIKE ? THEN 0 ELSE 1 END",
	})
	sql := `SELECT ` + searchColumns + `,
	   ` + rank + ` AS rank_key
	 FROM notes n
	 WHERE ` + where + `
	 ORDER BY
	   rank_key,
	   n.mtime DESC,
	   n.id DESC
	 LIMIT ?`
	args := []any{query, query + "%"}
	args = append(args, titleArgs...)
	args = append(args, limit)
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
			// A trailing "/" is how someone writes "everything under this", which is what the filter
			// already means — and left in place it means the opposite, since no tag is stored with one:
			// "#a/" would match nothing and quietly fall through to a full-text hunt for the literal text.
			tag := strings.TrimRight(strings.TrimSpace(strings.TrimPrefix(field, "#")), "/")
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
// filters (AND) with the same AND/OR title matching used for a plain query. Tags are hierarchical:
// #a matches a note tagged "a" or any descendant like "a/b", but never "ab" — the same rule the query
// evaluator applies (see query.TagMatches).
func searchTagged(parsed parsedTaggedQuery, limit int) (string, []any) {
	where := []string{"n.kind IN ('note', 'journal')"}
	var whereArgs []any
	for _, tag := range parsed.Tags {
		where = append(where, "EXISTS (SELECT 1 FROM tags t WHERE t.note_id = n.id AND (t.tag = ? COLLATE NOCASE OR t.tag LIKE ? || '/%'))")
		whereArgs = append(whereArgs, tag, tag)
	}
	if titleClause, titleArgs := titleMatchClause(parsed.Text); titleClause != "" {
		where = append(where, "("+titleClause+")")
		whereArgs = append(whereArgs, titleArgs...)
	}

	// Exact tag matches rank before descendant (prefix) matches. The whole vector is packed into one
	// selected column (see rankExpr) so a cross-vault merge can order on it; its args come first.
	var rankCases []string
	var rankArgs []any
	for _, tag := range parsed.Tags {
		rankCases = append(rankCases, `CASE WHEN EXISTS (
	     SELECT 1 FROM tags t WHERE t.note_id = n.id AND t.tag = ? COLLATE NOCASE
	   ) THEN 0 ELSE 1 END`)
		rankArgs = append(rankArgs, tag)
	}
	if parsed.Text != "" {
		rankCases = append(rankCases,
			"CASE WHEN n.title = ? COLLATE NOCASE THEN 0 ELSE 1 END",
			"CASE WHEN n.title LIKE ? THEN 0 ELSE 1 END",
		)
		rankArgs = append(rankArgs, parsed.Text, parsed.Text+"%")
	}

	sql := `SELECT n.id, n.kind, n.title, n.mtime, n.icon, n.seen_at, n.read_at,
	   COALESCE((
	     SELECT group_concat(tag, char(31))
	     FROM (SELECT tag FROM tags WHERE note_id = n.id ORDER BY tag)
	   ), '') AS tags,
	   ` + rankExpr(rankCases) + ` AS rank_key
	 FROM notes n
	 WHERE ` + strings.Join(where, " AND ") + `
	 ORDER BY
	   rank_key,
	   n.mtime DESC,
	   n.id DESC
	 LIMIT ?`
	args := append(rankArgs, whereArgs...)
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
		// bm25 is selected as well as ordered by, so a cross-vault merge (MergeSearchResults) orders on
		// the score the index computed instead of trying to reproduce it.
		`SELECT n.id, n.kind, n.title, n.mtime, n.icon, n.seen_at, n.read_at,
		   COALESCE((
		     SELECT group_concat(tag, char(31))
		     FROM (SELECT tag FROM tags WHERE note_id = n.id ORDER BY tag)
		   ), '') AS tags,
		   bm25(notes_fts) AS bm25_rank
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
		// The icon rides along like it does on the title path: without it the same note would show
		// one icon in the title group and the tag/kind fallback in the body group.
		if err := rows.Scan(&r.NoteID, &r.FileKind, &r.Title, &r.Mtime, &r.Icon, &r.SeenAt, &r.ReadAt, &tags, &r.Rank); err != nil {
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
		`SELECT n.id, n.kind, n.title, n.mtime, n.icon, n.seen_at, n.read_at,
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
		if err := rows.Scan(&r.NoteID, &r.FileKind, &r.Title, &r.Mtime, &r.Icon, &r.SeenAt, &r.ReadAt, &tags); err != nil {
			return nil, err
		}
		r.Tags = splitTags(tags)
		out = append(out, r)
	}
	return out, rows.Err()
}

// NewestRefs returns regular notes in creation order, newest first. Regular note ids are
// time-derived at creation, so the id is the precise ordering key; the sidecar's Created field is a
// display date whose format is configurable and may only have day precision. Journals are excluded:
// opening today's journal is activity, not creating a new note for the workspace's New listing.
func (s *Store) NewestRefs(limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.db.Query(
		`SELECT n.id, n.kind, n.title, n.mtime, n.icon, n.seen_at, n.read_at,
		   COALESCE((
		     SELECT group_concat(tag, char(31))
		     FROM (SELECT tag FROM tags WHERE note_id = n.id ORDER BY tag)
		   ), '') AS tags
		 FROM notes n
		 WHERE n.kind = 'note'
		 ORDER BY n.id DESC
		 LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SearchResult
	for rows.Next() {
		var r SearchResult
		var tags string
		if err := rows.Scan(&r.NoteID, &r.FileKind, &r.Title, &r.Mtime, &r.Icon, &r.SeenAt, &r.ReadAt, &tags); err != nil {
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

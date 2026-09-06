package query

import (
	"sort"
	"strconv"
	"strings"

	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

// NoteRow is the queryable projection of one note. Rows are the query domain: the whole index for
// the CLI and the live web workspace, the published set for a static export — so a published query
// can never leak an unpublished note.
type NoteRow struct {
	ID    int64
	Title string
	// Kind is the note's file kind ("note", "journal") — not queryable, but it feeds icon
	// resolution for gallery cards, which is per-kind.
	Kind  string
	Tags  []string
	Props []note.Prop
	Mtime int64
	// Body is the note's body text. Store-backed loads leave it empty and resolve body conditions
	// against the FTS5 index instead (see RunWithBody and StoreBodyResolver); surfaces that already
	// hold every body in memory (the static export, the pure tests) fill it and body conditions
	// match it directly.
	Body string
}

// Result is an evaluated query, ready for JSON output or Markdown rendering.
type Result struct {
	Columns []string `json:"columns"`
	Rows    []Row    `json:"rows"`
}

// Row is one matching note: its id, title (for linking), and one cell per requested column (a
// multi-valued property joins its values with ", ").
type Row struct {
	NoteID int64    `json:"note_id"`
	Title  string   `json:"title"`
	Cells  []string `json:"cells"`
}

// RowsFromStore loads every indexed note (and journal) as query rows, in the shared note-list order:
// most recently updated first, id ascending on ties.
func RowsFromStore(s *store.Store) ([]NoteRow, error) {
	refs, err := s.SearchRefs()
	if err != nil {
		return nil, err
	}
	props, err := s.AllProps()
	if err != nil {
		return nil, err
	}
	rows := make([]NoteRow, 0, len(refs))
	for _, r := range refs {
		rows = append(rows, NoteRow{ID: r.NoteID, Title: r.Title, Kind: r.FileKind, Tags: r.Tags, Props: props[r.NoteID], Mtime: r.Mtime})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Mtime != rows[j].Mtime {
			return rows[i].Mtime > rows[j].Mtime
		}
		return rows[i].ID < rows[j].ID
	})
	return rows, nil
}

// Run evaluates a parsed query over rows, preserving the input order unless SORT reorders it.
// Body conditions (body = "text" / body != "text") evaluate against the rows' own Body text — the
// in-memory path used by the static export and the unit tests.
func Run(q Query, rows []NoteRow) Result {
	res, _ := RunWithBody(q, rows, nil)
	return res
}

// RunWithBody evaluates a parsed query over rows like Run, resolving body conditions through
// resolve when non-nil (the store-backed FTS5 path, see StoreBodyResolver) and against the rows'
// own Body text when resolve is nil. Resolving through the store never mutates q or rows; a failed
// resolution is an error so the CLI can fail loudly and an embedded fence can render the inline
// query error.
func RunWithBody(q Query, rows []NoteRow, resolve BodyResolver) (Result, error) {
	body, err := resolveBodyConds(q.Where, resolve)
	if err != nil {
		return Result{}, err
	}
	res := Result{Columns: q.Columns, Rows: []Row{}}
	var matched []NoteRow
	for _, r := range rows {
		if q.From != "" && !hasTag(r.Tags, q.From) {
			continue
		}
		if !matchesAll(r, q.Where, body) {
			continue
		}
		matched = append(matched, r)
	}
	if q.Sort != "" {
		sortRows(matched, q.Sort, q.Desc)
	}
	if q.Limit > 0 && len(matched) > q.Limit {
		matched = matched[:q.Limit]
	}
	for _, r := range matched {
		row := Row{NoteID: r.ID, Title: r.Title}
		for _, col := range q.Columns {
			row.Cells = append(row.Cells, strings.Join(values(r, col), ", "))
		}
		res.Rows = append(res.Rows, row)
	}
	return res, nil
}

// TagMatches reports whether a note tag matches a tag filter hierarchically and case-insensitively:
// filter "a" matches tags "a" and "a/b" but not "ab".
func TagMatches(tag, filter string) bool {
	t, f := strings.ToLower(tag), strings.ToLower(filter)
	// "a/" is how someone spells "everything under a", which is what the filter already means; no tag
	// is stored with a trailing separator, so leaving it on would match nothing at all.
	f = strings.TrimRight(f, "/")
	return t == f || strings.HasPrefix(t, f+"/")
}

func hasTag(tags []string, filter string) bool {
	for _, t := range tags {
		if TagMatches(t, filter) {
			return true
		}
	}
	return false
}

// values returns a note's values for a key. props.<name> reads the flattened typed properties
// (sidecar props + inline fields); a bare key is a note attribute (title, tags). Parse has already
// rejected any other bare key, so an unknown bare key never reaches here. A note without a queried
// property yields no values (empty is a legitimate answer for a property, unlike an unknown key).
func values(r NoteRow, key string) []string {
	if name, ok := propName(key); ok {
		var out []string
		for _, p := range r.Props {
			if p.Key == name {
				out = append(out, p.Value)
			}
		}
		return out
	}
	switch key {
	case "title":
		return []string{r.Title}
	case "tags":
		return r.Tags
	}
	return nil
}

func matchesAll(r NoteRow, conds []Cond, body *bodyResult) bool {
	for _, c := range conds {
		if c.Key == "body" {
			// With a store-backed resolver the conditions were pre-resolved into note id sets and
			// the rows carry no Body text; with a nil resolver each row's Body is scanned in memory.
			if body != nil {
				if !body.ok(r.ID, c) {
					return false
				}
			} else if !bodyMatches(r.Body, c) {
				return false
			}
			continue
		}
		if !matches(r, c) {
			return false
		}
	}
	return true
}

// bodyResult is the store-backed resolution of a query's body conditions: the set of ids that
// satisfy every body = "text" condition (their intersection) and the set that fail every
// body != "text" condition (their union), combined with AND like the rest of WHERE.
type bodyResult struct {
	hasPositive bool
	positives   map[int64]bool // intersection of all body = sets
	negatives   map[int64]bool // union of all body != sets
}

// resolveBodyConds resolves every body condition in conds through resolve, or returns nil to keep
// the in-memory scan path (a nil resolve, or a query with no body conditions).
func resolveBodyConds(conds []Cond, resolve BodyResolver) (*bodyResult, error) {
	var br bodyResult
	for _, c := range conds {
		if c.Key != "body" {
			continue
		}
		if resolve == nil {
			return nil, nil // pure path: matchesAll reads each row's Body
		}
		ids, err := resolve(c.Value)
		if err != nil {
			return nil, err
		}
		if c.Op == "!=" {
			if br.negatives == nil {
				br.negatives = map[int64]bool{}
			}
			for id := range ids {
				br.negatives[id] = true
			}
			continue
		}
		if !br.hasPositive {
			br.hasPositive = true
			br.positives = ids
			continue
		}
		for id := range br.positives {
			if !ids[id] {
				delete(br.positives, id)
			}
		}
	}
	if !br.hasPositive && br.negatives == nil {
		return nil, nil
	}
	return &br, nil
}

// ok reports whether id satisfies one body condition given the pre-resolved sets.
func (b *bodyResult) ok(id int64, c Cond) bool {
	if c.Op == "!=" {
		return !b.negatives[id]
	}
	return b.positives[id]
}

// bodyMatches evaluates one body condition against a note's body text with the same grammar as
// full-text search: terms within the value join with AND (implicit), an uppercase OR separates
// alternatives, and each term matches as a case-insensitive substring — the exact semantics of the
// per-file scan fallback (search.bodyContainsAll), so the in-memory surfaces agree with the
// FTS5-indexed ones. An empty body matches no "=" expression and satisfies every "!=" one.
func bodyMatches(body string, c Cond) bool {
	lower := strings.ToLower(body)
	for _, terms := range store.BodyGroups(c.Value) {
		all := len(terms) > 0
		for _, term := range terms {
			if !strings.Contains(lower, strings.ToLower(term)) {
				all = false
				break
			}
		}
		if all {
			if c.Op == "!=" {
				return false
			}
			return true
		}
	}
	return c.Op == "!="
}

// matches evaluates one condition. Multi-valued keys use any-of semantics for "=", "<", ">" and
// none-of for "!=" (a note without the key trivially satisfies "!="). A bare key is a presence check.
func matches(r NoteRow, c Cond) bool {
	if c.Tag != "" {
		return hasTag(r.Tags, c.Tag)
	}
	vals := values(r, c.Key)
	switch c.Op {
	case "":
		return len(vals) > 0
	case "!=":
		for _, v := range vals {
			if compare(v, c.Value) == 0 {
				return false
			}
		}
		return true
	default:
		for _, v := range vals {
			cmp := compare(v, c.Value)
			if (c.Op == "=" && cmp == 0) || (c.Op == "<" && cmp < 0) || (c.Op == ">" && cmp > 0) {
				return true
			}
		}
		return false
	}
}

// compare orders two value texts by type: both numbers compare numerically, everything else
// case-insensitively as text — which orders ISO dates (YYYY-MM-DD) chronologically for free.
func compare(a, b string) int {
	if fa, errA := strconv.ParseFloat(a, 64); errA == nil {
		if fb, errB := strconv.ParseFloat(b, 64); errB == nil {
			switch {
			case fa < fb:
				return -1
			case fa > fb:
				return 1
			default:
				return 0
			}
		}
	}
	return strings.Compare(strings.ToLower(a), strings.ToLower(b))
}

// sortRows stably sorts by the first value of key; notes without the key sort last in both directions.
func sortRows(rows []NoteRow, key string, desc bool) {
	sort.SliceStable(rows, func(i, j int) bool {
		vi, vj := values(rows[i], key), values(rows[j], key)
		if len(vi) == 0 || len(vj) == 0 {
			return len(vi) > 0 // rows missing the key sink to the end
		}
		if desc {
			return compare(vi[0], vj[0]) > 0
		}
		return compare(vi[0], vj[0]) < 0
	})
}

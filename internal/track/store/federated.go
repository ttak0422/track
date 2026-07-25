package store

import (
	"database/sql"
	"fmt"
	"strings"
)

// FederatedVault names one vault's index database for a federated connection: the registry name
// that labels its rows ("" for the unregistered active vault) and the database path.
type FederatedVault struct {
	Name   string
	DBPath string
}

// Federated is a read-only view over several vaults' index databases ATTACHed to one connection
// (multi-vault phase 2). Physical DBs stay one-per-vault; only the query crosses them, labeling
// each row with its vault so (vault, id) stays unambiguous even when ids collide across vaults.
type Federated struct {
	db     *sql.DB
	vaults []fedVault
}

// fedVault pairs a generated schema alias with the vault name it labels rows with. Aliases are
// v0, v1, ... so schema identifiers never need quoting regardless of vault names.
type fedVault struct {
	alias string
	name  string
}

// OpenFederated attaches every vault's index database to one in-memory connection. Callers
// self-heal each vault (RefreshIfStale) first, so the attached DBs exist and are fresh; SQLite's
// attach limit (10 by default) bounds the registry size this can serve.
// ponytail: databases attach read-write and discipline keeps this SELECT-only; switch to mode=ro
// URIs if a write ever sneaks in.
func OpenFederated(vaults []FederatedVault) (*Federated, error) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec("PRAGMA busy_timeout = 5000;"); err != nil {
		db.Close()
		return nil, err
	}
	f := &Federated{db: db}
	for i, v := range vaults {
		alias := fmt.Sprintf("v%d", i)
		if _, err := db.Exec(fmt.Sprintf("ATTACH DATABASE ? AS %s", alias), v.DBPath); err != nil {
			db.Close()
			return nil, fmt.Errorf("attach vault %q index: %w", v.Name, err)
		}
		f.vaults = append(f.vaults, fedVault{alias: alias, name: v.Name})
	}
	return f, nil
}

func (f *Federated) Close() error {
	return f.db.Close()
}

// Search is the federated counterpart of SearchScoped for title/tag queries: the same grammar and
// ranking, run per attached vault and merged by one global ORDER BY, each row labeled with its
// vault. The trailing vault tiebreak keeps cross-vault ordering deterministic when mtime and id
// collide.
func (f *Federated) Search(query string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 50
	}
	parsed, _ := parseTaggedQuery(query)

	// Rank expressions become selected columns (r0, r1, ...) so the outer ORDER BY can rank the
	// union globally. Their args appear in each subquery's select list, before the WHERE args.
	var rankCols []string
	var rankArgs []any
	for _, tag := range parsed.Tags {
		rankCols = append(rankCols, `CASE WHEN EXISTS (
	     SELECT 1 FROM {p}tags t WHERE t.note_id = n.id AND t.tag = ? COLLATE NOCASE
	   ) THEN 0 ELSE 1 END`)
		rankArgs = append(rankArgs, tag)
	}
	if parsed.Text != "" {
		rankCols = append(rankCols,
			"CASE WHEN n.title = ? COLLATE NOCASE THEN 0 ELSE 1 END",
			"CASE WHEN n.title LIKE ? THEN 0 ELSE 1 END",
		)
		rankArgs = append(rankArgs, parsed.Text, parsed.Text+"%")
	}

	where := []string{"n.kind IN ('note', 'journal')"}
	var whereArgs []any
	for _, tag := range parsed.Tags {
		where = append(where, "EXISTS (SELECT 1 FROM {p}tags t WHERE t.note_id = n.id AND (t.tag = ? COLLATE NOCASE OR t.tag LIKE ? || '/%'))")
		whereArgs = append(whereArgs, tag, tag)
	}
	if clause, args := titleMatchClause(parsed.Text); clause != "" {
		where = append(where, "("+clause+")")
		whereArgs = append(whereArgs, args...)
	}

	selectCols := []string{
		"? AS vault", "n.id AS id", "n.kind AS kind", "n.title AS title", "n.mtime AS mtime", "n.icon AS icon",
		`COALESCE((
	     SELECT group_concat(tag, char(31))
	     FROM (SELECT tag FROM {p}tags WHERE note_id = n.id ORDER BY tag)
	   ), '') AS tags`,
	}
	for i, col := range rankCols {
		selectCols = append(selectCols, fmt.Sprintf("%s AS r%d", col, i))
	}
	sub := "SELECT " + strings.Join(selectCols, ",\n	   ") + "\n	 FROM {p}notes n\n	 WHERE " + strings.Join(where, " AND ")

	var subs []string
	var args []any
	for _, v := range f.vaults {
		subs = append(subs, strings.ReplaceAll(sub, "{p}", v.alias+"."))
		args = append(args, v.name)
		args = append(args, rankArgs...)
		args = append(args, whereArgs...)
	}
	order := make([]string, 0, len(rankCols)+3)
	for i := range rankCols {
		order = append(order, fmt.Sprintf("r%d", i))
	}
	order = append(order, "mtime DESC", "id DESC", "vault")
	query = "SELECT vault, id, kind, title, mtime, icon, tags FROM (\n" +
		strings.Join(subs, "\nUNION ALL\n") +
		"\n) ORDER BY " + strings.Join(order, ", ") + " LIMIT ?"
	args = append(args, limit)

	return f.scanResults(query, args, true)
}

// Recent is the federated counterpart of SearchRefs for an empty query: every vault's notes merged
// into one most-recently-updated-first listing, each row labelled with its vault. The trailing vault
// tiebreak keeps the order deterministic when mtime and id collide across vaults.
func (f *Federated) Recent(limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 50
	}
	sub := `SELECT ? AS vault, n.id AS id, n.kind AS kind, n.title AS title, n.mtime AS mtime, n.icon AS icon,
	   COALESCE((
	     SELECT group_concat(tag, char(31))
	     FROM (SELECT tag FROM {p}tags WHERE note_id = n.id ORDER BY tag)
	   ), '') AS tags
	 FROM {p}notes n
	 WHERE n.kind IN ('note', 'journal')`

	var subs []string
	var args []any
	for _, v := range f.vaults {
		subs = append(subs, strings.ReplaceAll(sub, "{p}", v.alias+"."))
		args = append(args, v.name)
	}
	query := "SELECT vault, id, kind, title, mtime, icon, tags FROM (\n" +
		strings.Join(subs, "\nUNION ALL\n") +
		"\n) ORDER BY mtime DESC, id DESC, vault LIMIT ?"
	args = append(args, limit)

	return f.scanResults(query, args, true)
}

// SearchBodyFTS is the federated counterpart of Store.SearchBodyFTS: each vault's trigram FTS index
// matches independently and the union orders by bm25 then recency. bm25 scores from different
// indexes are only approximately comparable (same tokenizer and weights), which is accepted for the
// merged ranking. Caller must ensure BodyQueryUsesFTS(query).
func (f *Federated) SearchBodyFTS(query string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 50
	}
	groups := BodyGroups(query)
	if len(groups) == 0 {
		return nil, nil
	}
	match := ftsMatchExprGroups(groups)

	// The FTS table stays unaliased: FTS5's MATCH and bm25() address the table through its hidden
	// same-named column, which an alias would hide (an attached v0.notes_fts is still referenced as
	// plain notes_fts inside the subquery).
	sub := `SELECT ? AS vault, n.id AS id, n.kind AS kind, n.title AS title, n.mtime AS mtime,
	   COALESCE((
	     SELECT group_concat(tag, char(31))
	     FROM (SELECT tag FROM {p}tags WHERE note_id = n.id ORDER BY tag)
	   ), '') AS tags,
	   bm25(notes_fts) AS rank
	 FROM {p}notes_fts
	 JOIN {p}notes n ON n.id = notes_fts.rowid
	 WHERE notes_fts MATCH ? AND n.kind IN ('note', 'journal')`

	var subs []string
	var args []any
	for _, v := range f.vaults {
		subs = append(subs, strings.ReplaceAll(sub, "{p}", v.alias+"."))
		args = append(args, v.name, match)
	}
	sql := "SELECT vault, id, kind, title, mtime, tags FROM (\n" +
		strings.Join(subs, "\nUNION ALL\n") +
		"\n) ORDER BY rank, mtime DESC, id DESC, vault LIMIT ?"
	args = append(args, limit)

	return f.scanResults(sql, args, false)
}

// scanResults runs a federated query whose select list is the SearchResult columns, with or
// without the icon column.
func (f *Federated) scanResults(query string, args []any, withIcon bool) ([]SearchResult, error) {
	rows, err := f.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SearchResult
	for rows.Next() {
		var r SearchResult
		var tags string
		var err error
		if withIcon {
			err = rows.Scan(&r.Vault, &r.NoteID, &r.FileKind, &r.Title, &r.Mtime, &r.Icon, &tags)
		} else {
			err = rows.Scan(&r.Vault, &r.NoteID, &r.FileKind, &r.Title, &r.Mtime, &tags)
		}
		if err != nil {
			return nil, err
		}
		r.Tags = splitTags(tags)
		out = append(out, r)
	}
	return out, rows.Err()
}

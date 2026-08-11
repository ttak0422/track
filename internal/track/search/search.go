// Package search composes the store's title and body search into the result list every frontend
// shows: title matches first, then full-text matches for notes the titles did not already name.
//
// It lives here rather than in internal/cli because the composition is engine work, not a command:
// the store answers title and body separately (SearchScoped is title-only; body goes through
// SearchBodyFTS), and reading the matched files for a line number and snippet needs config. The web
// workspace is the second caller — it could not reuse the CLI's copy, since internal/cli imports the
// web server.
package search

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

// Vault is one reachable vault a cross-vault search reads: the name that labels its rows ("" for a
// vault with no registry name), its config, and an open index handle. The caller supplies the name
// because the CLI labels by registry name while the web server labels by the name a view was opened
// under.
type Vault struct {
	Name  string
	Cfg   *config.Config
	Store *store.Store
}

// Failed is one vault whose own search returned an error. A cross-vault search reports it and
// answers from the vaults that could reply: ADR 0062 gives a vault exactly two outcomes in a
// cross-vault read, searched or unavailable with a reason, and a query that errored is the second
// one just as much as a vault that could not be opened. Vault is the label the caller handed in, so
// the caller can map it back to whatever it reports gaps as.
type Failed struct {
	Vault string
	Err   error
}

// vaultKey is the (vault, id) identity a cross-vault result needs: the same numeric id can name
// different notes in different vaults.
type vaultKey struct {
	vault string
	id    int64
}

// AddPaths fills each result's file path, which the store leaves empty (a path is a serving-layer
// concern, derived from the vault config).
func AddPaths(cfg *config.Config, results []store.SearchResult) {
	for i := range results {
		results[i].Path = cfg.PathForKind(results[i].FileKind, results[i].NoteID)
	}
}

func Scoped(cfg *config.Config, s *store.Store, query string, limit int, scope store.SearchScope) ([]store.SearchResult, error) {
	if limit <= 0 {
		limit = 50
	}
	switch scope {
	case store.SearchTitle:
		results, err := s.SearchScoped(query, limit, scope)
		AddPaths(cfg, results)
		mark(results, MatchTitle)
		return results, err
	case store.SearchAll:
		results, err := s.SearchScoped(query, limit, scope)
		if err != nil {
			return nil, err
		}
		AddPaths(cfg, results)
		mark(results, MatchTitle)
		seen := make(map[int64]bool, len(results))
		for _, result := range results {
			seen[result.NoteID] = true
		}
		body, err := bodySearchResults(cfg, s, query, limit-len(results), seen)
		if err != nil {
			return nil, err
		}
		// ponytail: title hits spend the shared budget first, so a query matching `limit` titles
		// leaves the body group empty. Give each group its own limit if that ever shows up.
		results = append(results, body...)
		for _, result := range body {
			seen[result.NoteID] = true
		}
		// Last, and at most one: naming a file is the coarsest of the three ways to ask for a note,
		// so it goes after what the query said about the note's own words.
		path, err := pathSearchResults(cfg, s, query, limit-len(results), seen)
		if err != nil {
			return nil, err
		}
		return append(results, path...), nil
	case store.SearchBody:
		return bodySearchResults(cfg, s, query, limit, nil)
	case store.SearchPath:
		return pathSearchResults(cfg, s, query, limit, nil)
	default:
		return nil, fmt.Errorf("unknown search scope %q", scope)
	}
}

// bodySearchResults finds notes whose body matches every query term. It prefers the FTS5 index (fast,
// bm25-ranked) and falls back to a per-file scan only for queries with a term too short to form a
// trigram (see store.BodyQueryUsesFTS), so short and two-character CJK queries still work. Either path
// then locates the first matching line in the file to preserve the line-number + snippet contract.
// Match values, the discriminator a frontend groups on.
const (
	MatchTitle = "title"
	MatchBody  = "body"
	MatchPath  = "path"
)

// NoteIDFromFileName reads the note id out of a query that names a file. A coding agent that has been
// reading the vault refers to notes the way the filesystem does — "note/1785024006000.md", or just the
// file name, or the bare id — and every kind's file is named from the id (config.PathForKind), so all
// three are the same lookup.
//
// The rule is deliberately exact rather than a substring match on the path: ids are timestamps, so a
// substring rule would turn any digit run into a shower of near-misses. Naming a file is naming one
// note, and anything that is not a whole id is left to the title and body searches.
func NoteIDFromFileName(query string) (int64, bool) {
	name := strings.TrimSpace(query)
	if name == "" {
		return 0, false
	}
	// Both separators: an agent on Windows, or one quoting a path it read from a Go error, may hand
	// over either.
	name = name[strings.LastIndexAny(name, `/\`)+1:]
	name = strings.TrimSuffix(name, ".md")
	if name == "" {
		return 0, false
	}
	for _, r := range name {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	id, err := strconv.ParseInt(name, 10, 64)
	if err != nil {
		return 0, false // a run of digits too long for an id names no note
	}
	return id, true
}

// pathSearchResults returns the note whose file the query names, or nothing. skip drops a note the
// title or body groups already listed, so one note never appears twice in a composed list.
func pathSearchResults(cfg *config.Config, s *store.Store, query string, limit int, skip map[int64]bool) ([]store.SearchResult, error) {
	if limit <= 0 {
		return nil, nil
	}
	id, ok := NoteIDFromFileName(query)
	if !ok || skip[id] {
		return nil, nil
	}
	results, err := s.SearchByID(id)
	if err != nil {
		return nil, err
	}
	AddPaths(cfg, results)
	mark(results, MatchPath)
	return results, nil
}

func mark(results []store.SearchResult, match string) {
	for i := range results {
		results[i].Match = match
	}
}

func bodySearchResults(cfg *config.Config, s *store.Store, query string, limit int, skip map[int64]bool) ([]store.SearchResult, error) {
	if limit <= 0 {
		return []store.SearchResult{}, nil
	}
	groups := store.BodyGroups(query)
	if len(groups) == 0 {
		return []store.SearchResult{}, nil
	}
	var (
		out []store.SearchResult
		err error
	)
	if store.BodyQueryUsesFTS(query) {
		out, err = bodySearchFTS(cfg, s, query, limit, skip)
	} else {
		out, err = bodySearchScan(cfg, s, groups, limit, skip)
	}
	mark(out, MatchBody)
	return out, err
}

// bodySearchFTS serves a body query from the FTS index, keeping its relevance order and reading only
// the matched files to attach a line number and snippet.
func bodySearchFTS(cfg *config.Config, s *store.Store, query string, limit int, skip map[int64]bool) ([]store.SearchResult, error) {
	// Over-fetch by the skip count so notes already returned as title hits do not shrink the page.
	hits, err := s.SearchBodyFTS(query, limit+len(skip))
	if err != nil {
		return nil, err
	}
	groups := store.BodyGroups(query)
	out := make([]store.SearchResult, 0, len(hits))
	for _, hit := range hits {
		if skip[hit.NoteID] {
			continue
		}
		hit.Path = cfg.PathForKind(hit.FileKind, hit.NoteID)
		hit.Line, hit.Snippet = fileLineMatchGroups(hit.Path, groups)
		out = append(out, hit)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// bodySearchScan is the short-term fallback: it scans note files for one containing every term, then
// sorts by recency (bm25 is unavailable off the index). It is only reached for queries the trigram
// index cannot serve, so full scans stay off the common path.
func bodySearchScan(cfg *config.Config, s *store.Store, groups [][]string, limit int, skip map[int64]bool) ([]store.SearchResult, error) {
	notes, err := s.SearchRefs()
	if err != nil {
		return nil, err
	}
	refs := make(map[int64]store.SearchResult, len(notes))
	for _, n := range notes {
		refs[n.NoteID] = n
	}
	paths, err := scanSearchFiles(cfg)
	if err != nil {
		return nil, err
	}
	var out []store.SearchResult
	for _, path := range paths {
		id, err := note.IDFromPath(path)
		ref, indexed := refs[id]
		if err != nil || !indexed || skip[id] {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		body, _, _ := note.SplitLegacyFootmatter(string(raw))
		if !bodyMatchesAnyGroup(body, groups) {
			continue
		}
		ref.Path = cfg.PathForKind(ref.FileKind, id)
		ref.Line, ref.Snippet = bodyLineMatchGroups(body, groups)
		out = append(out, ref)
	}
	Sort(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func Sort(results []store.SearchResult) {
	slices.SortFunc(results, func(a, b store.SearchResult) int {
		if a.Mtime != b.Mtime {
			return cmpDesc(a.Mtime, b.Mtime)
		}
		return cmpDesc(a.NoteID, b.NoteID)
	})
}

func cmpDesc[T ~int64](a, b T) int {
	switch {
	case a > b:
		return -1
	case a < b:
		return 1
	default:
		return 0
	}
}

func scanSearchFiles(cfg *config.Config) ([]string, error) {
	var out []string
	for _, root := range []string{cfg.NoteDir(), cfg.JournalDir()} {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				if d == nil {
					return nil
				}
				return err
			}
			if d.IsDir() {
				if path != root {
					return filepath.SkipDir
				}
				return nil
			}
			if slices.Contains(cfg.Extensions, filepath.Ext(path)) {
				out = append(out, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	slices.Sort(out)
	return out, nil
}

// bodyContainsAll reports whether body contains every term as a case-insensitive substring (implicit
// AND), matching the trigram FTS semantics so the scan fallback agrees with the indexed path.
func bodyContainsAll(body string, terms []string) bool {
	lowerBody := strings.ToLower(body)
	for _, term := range terms {
		if !strings.Contains(lowerBody, strings.ToLower(term)) {
			return false
		}
	}
	return true
}

// bodyMatchesAnyGroup reports whether body satisfies any one OR group (all of that group's terms
// present), mirroring the FTS "(a AND b) OR (c)" semantics for the scan fallback.
func bodyMatchesAnyGroup(body string, groups [][]string) bool {
	for _, terms := range groups {
		if bodyContainsAll(body, terms) {
			return true
		}
	}
	return false
}

// bodyLineMatchGroups returns the 1-based line and snippet best representing the match: the first line
// that contains every term of some satisfied OR group (the tightest match), else the first line
// containing any query term. It returns (0, "") when no line holds a term — the title-only sentinel,
// reached only when a group's terms straddle line breaks.
func bodyLineMatchGroups(body string, groups [][]string) (int, string) {
	anyLine, anyText := 0, ""
	for i, line := range strings.Split(body, "\n") {
		lowerLine := strings.ToLower(line)
		for _, terms := range groups {
			all, any := len(terms) > 0, false
			for _, term := range terms {
				if strings.Contains(lowerLine, strings.ToLower(term)) {
					any = true
				} else {
					all = false
				}
			}
			if all {
				return i + 1, truncateSearchSnippet(strings.TrimSpace(line), 120)
			}
			if any && anyLine == 0 {
				anyLine, anyText = i+1, truncateSearchSnippet(strings.TrimSpace(line), 120)
			}
		}
	}
	return anyLine, anyText
}

// fileLineMatchGroups reads path and locates the best matching line for the query's OR groups. A read
// error yields the title-only sentinel rather than failing the search: the FTS hit is authoritative.
func fileLineMatchGroups(path string, groups [][]string) (int, string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, ""
	}
	body, _, _ := note.SplitLegacyFootmatter(string(raw))
	return bodyLineMatchGroups(body, groups)
}

func truncateSearchSnippet(s string, max int) string {
	if len(s) <= max {
		return s
	}
	end := max
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end] + "…"
}

// Federated mirrors Scoped across vaults, but keeps its two phases apart: title hits from
// every vault merge into one page, then body hits from every vault merge into another. Merging each
// vault's already-composed title-then-body list instead would interleave bm25-ranked body hits with
// title hits, which are ranked on a different scale. Hits are deduplicated by (vault, id).
//
// A vault whose own query fails comes back in the second value rather than sinking the search; the
// error return is reserved for a caller mistake — an unknown scope — that no vault can degrade.
func Federated(vaults []Vault, query string, limit int, scope store.SearchScope) ([]store.SearchResult, []Failed, error) {
	switch scope {
	case store.SearchTitle:
		results, failed := federatedTitle(vaults, query, limit)
		return results, failed, nil
	case store.SearchAll:
		results, failed := federatedTitle(vaults, query, limit)
		seen := make(map[vaultKey]bool, len(results))
		for _, r := range results {
			seen[vaultKey{r.Vault, r.NoteID}] = true
		}
		// A vault that already failed its title query is not asked again: it would fail the same way
		// and be reported twice.
		body, bodyFailed := federatedBody(without(vaults, failed), query, limit-len(results), seen)
		results = append(results, body...)
		failed = append(failed, bodyFailed...)
		for _, r := range body {
			seen[vaultKey{r.Vault, r.NoteID}] = true
		}
		// Ids are vault-local, so a file name can name a note in more than one vault; each answers for
		// itself, the same way the title and body groups do.
		path, pathFailed := federatedPath(without(vaults, failed), query, limit-len(results), seen)
		return append(results, path...), append(failed, pathFailed...), nil
	case store.SearchBody:
		results, failed := federatedBody(vaults, query, limit, nil)
		return results, failed, nil
	case store.SearchPath:
		results, failed := federatedPath(vaults, query, limit, nil)
		return results, failed, nil
	default:
		return nil, nil, fmt.Errorf("unknown search scope %q", scope)
	}
}

// without drops the vaults that already failed a phase. Vaults are matched by name, which the
// registry keeps unique.
func without(vaults []Vault, failed []Failed) []Vault {
	out := make([]Vault, 0, len(vaults))
	for _, v := range vaults {
		if !slices.ContainsFunc(failed, func(f Failed) bool { return f.Vault == v.Name }) {
			out = append(out, v)
		}
	}
	return out
}

// federatedTitleResults runs the single-vault title query in every vault, labels and resolves each
// hit against the vault it came from, and merges the pages into the global top-k.
func federatedTitle(vaults []Vault, query string, limit int) ([]store.SearchResult, []Failed) {
	pages := make([][]store.SearchResult, 0, len(vaults))
	var failed []Failed
	for _, v := range vaults {
		page, err := v.Store.SearchScoped(query, limit, store.SearchTitle)
		if err != nil {
			failed = append(failed, Failed{Vault: v.Name, Err: err})
			continue
		}
		for i := range page {
			page[i].Vault = v.Name
			page[i].Path = v.Cfg.PathForKind(page[i].FileKind, page[i].NoteID)
		}
		mark(page, MatchTitle)
		pages = append(pages, page)
	}
	return store.MergeSearchResults(pages, limit), failed
}

// federatedBodyResults is the cross-vault counterpart of bodySearchResults, and runs exactly that per
// vault — so the FTS path, the short-term scan fallback, and the line/snippet lookup all stay in one
// place. Already-returned title hits are skipped per vault, since ids only mean anything inside one.
func federatedPath(vaults []Vault, query string, limit int, seen map[vaultKey]bool) ([]store.SearchResult, []Failed) {
	if limit <= 0 {
		return []store.SearchResult{}, nil
	}
	if _, ok := NoteIDFromFileName(query); !ok {
		return []store.SearchResult{}, nil
	}
	pages := make([][]store.SearchResult, 0, len(vaults))
	var failed []Failed
	for _, v := range vaults {
		skip := map[int64]bool{}
		for key := range seen {
			if key.vault == v.Name {
				skip[key.id] = true
			}
		}
		page, err := pathSearchResults(v.Cfg, v.Store, query, limit, skip)
		if err != nil {
			failed = append(failed, Failed{Vault: v.Name, Err: err})
			continue
		}
		for i := range page {
			page[i].Vault = v.Name
		}
		pages = append(pages, page)
	}
	return store.MergeSearchResults(pages, limit), failed
}

func federatedBody(vaults []Vault, query string, limit int, seen map[vaultKey]bool) ([]store.SearchResult, []Failed) {
	if limit <= 0 {
		return []store.SearchResult{}, nil
	}
	if len(store.BodyGroups(query)) == 0 {
		return []store.SearchResult{}, nil
	}
	pages := make([][]store.SearchResult, 0, len(vaults))
	var failed []Failed
	for _, v := range vaults {
		skip := map[int64]bool{}
		for key := range seen {
			if key.vault == v.Name {
				skip[key.id] = true
			}
		}
		page, err := bodySearchResults(v.Cfg, v.Store, query, limit, skip)
		if err != nil {
			failed = append(failed, Failed{Vault: v.Name, Err: err})
			continue
		}
		for i := range page {
			page[i].Vault = v.Name
		}
		pages = append(pages, page)
	}
	return store.MergeSearchResults(pages, limit), failed
}

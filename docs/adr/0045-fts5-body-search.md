# 0045. Full-text body search via SQLite FTS5 with a trigram tokenizer

Status: Accepted

## Context

`track search --scope body` used to grep every note file on each query: it read the whole vault
from disk, ran a case-insensitive substring match line by line, and sorted the hits by recency. That
is O(vault) per query and has no relevance ranking. The SQLite index already caches titles, tags,
and the link graph, and every read path self-heals through `RefreshIfStale` before serving a query,
so body text is the one search surface still living entirely on disk.

The driver is `modernc.org/sqlite` (pure Go, no cgo — chosen so the binary stays statically
buildable). FTS5 is a compile-time option in SQLite, so the first question was whether this driver
ships it. It does: `CREATE VIRTUAL TABLE ... USING fts5(...)` works out of the box at the pinned
version with no build tags, extensions, or driver change. That removes the only reason the feature
might have needed a dependency change.

## Decision

- **Store the body in an FTS5 virtual table.** `notes_fts(body)` keyed by `rowid = note id`, kept in
  step with the `notes` row inside the same `UpsertNote` transaction (delete-then-insert) and dropped
  in `DeleteNote`. Because it is part of the same rebuildable cache, the existing
  reindex / `RefreshIfStale` path repopulates it — a stale index still yields correct results after
  the self-heal reindex, with no separate "rebuild search" step. The schema version is bumped so
  existing indexes rebuild once and pick up the new table.
- **Tokenize on trigrams, not words.** The `trigram` tokenizer gives case-insensitive *substring*
  matching, which preserves the old grep semantics and — unlike the word-oriented `unicode61` — works
  for scripts without spaces (Japanese, Chinese), where a whole run would otherwise be a single token.
  Multi-term queries stay implicit-AND: each term is quoted as an FTS5 string literal (so user
  punctuation is never parsed as query syntax) and joined with `AND`.
- **Rank by bm25.** Body results come back in FTS5 relevance order (mtime/id as tiebreak), replacing
  the pure recency sort. This is the "with ranking" the grep lacked.
- **Fall back to a scan for short terms.** The trigram tokenizer cannot index or match a term shorter
  than three characters — including common two-character CJK words like 世界. Rather than silently
  return nothing, a query with any sub-trigram term routes to the original per-file scan (kept for
  exactly this purpose). Only very short queries pay that cost; everything else stays on the index.
- **Line numbers stay a file concern.** FTS ranks and matches; it does not track line offsets. The
  caller reads only the matched files (a handful, not the whole vault) and locates the first line
  holding all terms — else the first line holding any — to preserve the existing line-number +
  snippet output contract. The scan fallback uses the same locator, so both paths agree.

## Consequences

- Body search is now index-backed and ranked; the full-vault read happens only for genuinely short
  queries.
- The body is duplicated into the FTS table (it is not otherwise stored in the index). This is
  acceptable for a rebuildable cache and keeps CRUD trivial — a normal FTS5 table, not a
  contentless/external-content one, so deletes and updates are ordinary statements.
- Semantics shifted subtly and deliberately: a multi-term body query is now AND-across-the-note
  (every term appears somewhere) rather than requiring the whole query string on one line, matching
  how the FTS index behaves. Text inside fenced code blocks is indexed because the body is stored
  verbatim.
- The trigram three-character floor is the one sharp edge; it is contained by the fallback and
  documented in the Searching notes help page.

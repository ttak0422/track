# 0033: A tiny note query language, expanded through generated fence blocks

## Status

Accepted

## Context

The index stores typed note properties (ADR 0032) and tags, but the only read paths were search and
per-note listings. We want Dataview-style tables — "open projects sorted by due date" — available
from the CLI, inside a note in the live web workspace, and on the static export, without a query
engine dependency or a second grammar per surface.

## Decision

### One evaluator, three surfaces

`internal/track/query` owns the language end to end: a hand-rolled lexer/parser (no
parser-generator dependency), an evaluator over a plain `NoteRow` slice (id, title, tags, flattened
props, mtime), and a Markdown-table renderer. Every surface builds rows and calls the same
`Parse`/`Run`:

- `track query "<expr>"` and the live `/api/render` load rows from the SQLite index
  (`RowsFromStore`: one notes scan + one props scan, shared note-list order).
- The static export builds rows from the published docs only, so a published query can never leak
  or link an unpublished note.

Evaluation is an in-memory scan, not SQL: vaults are personal-sized, and one Go evaluator keeps the
semantics identical for the store-backed and doc-backed domains.

### The grammar

```
query = "TABLE" key ("," key)*
        [ "FROM" "#"tag ]
        [ "WHERE" cond ("AND" cond)* ]
        [ "SORT" key ["DESC"] ]
        [ "LIMIT" n ]
cond  = "#"tag | key op value | key      (bare key = presence check)
op    = "=" | "!=" | "<" | ">"
value = "quoted string" | bareword
```

Keywords are uppercase, so keys/values are always lowercase-safe; there is no OR, no functions, no
expressions — deliberately. `title` and `tags` are pseudo-keys next to the property keys. Values
compare typed: numbers numerically, everything else case-insensitive text (ISO dates order
chronologically for free). Multi-valued keys: `=`/`<`/`>` are any-of, `!=` is none-of (a note
without the key satisfies `!=`).

### Generated fence blocks

A ```` ```track-query ```` fence renders as its result table wherever notes render. The mechanism
is the viewspec fence resolver's expansion path, generalized exactly one step:
`babel.ReplaceBlocks(body, lang, replace)` splices replacement lines over every fence of one
language. `site` uses it for `viewspec → echarts` at build time and for `track-query → Markdown
table`; the live server expands `track-query` in `/api/render`. A future generated-block type (say
```` ```track-graph ````) plugs in as one more `ReplaceBlocks` call at those two call sites — no new
frontend component, since the replacement is plain Markdown the renderer already draws. Errors
follow the viewspec convention: an inline `> Query error: ...` plus the source, so the note still
renders.

Queries expand to GFM tables (title cells as `[[Title]]` wiki links) rather than a bespoke
component: both frontends already render tables and resolve wiki links.

### Saved queries, tag hierarchy, tag pages

- Config `queries:` maps names to expressions; `track query --saved <name>` and a `saved: <name>`
  fence body reference them. Directory exports have no vault config, so `saved:` does not resolve
  there.
- Tags are hierarchical: `#a` matches `a` and `a/b`, never `ab` — the same rule in the query
  evaluator, store search (`t.tag = ? OR t.tag LIKE ? || '/%'`), and the static-mode search filter.
  This replaces the old substring tag match.
- `/tags/<tag>` pages list a tag's (and descendants') notes, derived client-side from the existing
  notes listing in both deployments; the export writes a real file per used tag and ancestor.
- Directory sites (help/docs) lift a `tags:: a, b` inline field into page tags, since plain
  Markdown files have no sidecar.

## Consequences

- One more schema-free read path over the index; no SQLite schema change was needed.
- The `#tag` search semantics changed from substring to hierarchical prefix — stricter, and
  consistent with queries.
- Fences inside transcluded regions still show as source on the static site (same pre-existing
  limitation as viewspec fences).
- The grammar is small enough to be documented in full on one help page (docs/help/query.md); any
  future OR/functions should trigger a re-evaluation against a real parser before growing it.

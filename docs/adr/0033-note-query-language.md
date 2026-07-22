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
cond  = "#"tag | key op value | value op key | key      (bare key = presence check)
key   = attr | "props." name             (attr ∈ {title, tags})
op    = "=" | "!=" | "<" | ">"
value = "quoted string" | bareword
```

Keywords are uppercase, so keys/values are always lowercase-safe; there is no OR, no functions, no
expressions — deliberately. Values compare typed: numbers numerically, everything else
case-insensitive text (ISO dates order chronologically for free). Multi-valued keys: `=`/`<`/`>` are
any-of, `!=` is none-of (a note without the key satisfies `!=`).

**Two key namespaces, kept apart.** A *bare* identifier is a note-intrinsic attribute, and the set
is closed: `noteAttrs = {title, tags}` today, and it may grow. A *user property* (a sidecar prop or
inline field, ADR 0032) is reachable only as `props.<name>` — never as a bare word. This split is
deliberate:

- **No shadowing.** Before, `title`/`tags` were note-backed pseudo-keys and every other bare word hit
  the props; a property literally named `title` was silently unreachable. Now `props.title` reads the
  property and bare `title` reads the attribute — they never collide.
- **Growth-safe.** Because properties are quarantined under `props.`, adding a new bare attribute
  (say `mtime`) can never clash with someone's property of the same name.
- **Loud, never silent.** An unknown bare key is a parse error —
  `unknown key "status": note attributes are title, tags; query a property as props.status` — instead
  of a silently empty column. These queries are largely agent-authored, and an agent cannot see a
  silently-wrong result. `props.` with no name errors too; an absent *property* still yields empty
  (a note legitimately may not carry it). Validation lives at the single `parser.key` choke point
  every column/sort/condition key passes through; `props.` is stripped in the rendered table header
  (`props.status` → `status`) but kept verbatim in the CLI JSON `columns`.

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
- Directory sites (help/docs) take page tags from the site config's `pages` entry (`site.yml`,
  ADR 0049), since plain Markdown files have no sidecar and note-level metadata never goes in a
  body (ADR 0032).

## Consequences

- One more schema-free read path over the index; no SQLite schema change was needed.
- The `#tag` search semantics changed from substring to hierarchical prefix — stricter, and
  consistent with queries.
- Fences inside transcluded regions still show as source on the static site (same pre-existing
  limitation as viewspec fences).
- The grammar is small enough to be documented in full on one help page (docs/help/query.md); any
  future OR/functions should trigger a re-evaluation against a real parser before growing it.

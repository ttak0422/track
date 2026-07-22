# Query

Queries turn the vault into a small database: pick columns, filter by tags and typed
[[Properties]], sort, and get a table back. One engine evaluates the same expression everywhere —
the `track query` CLI command, an embedded `track-query` block in the [[Web workspace]], and the
published static site. If you know Obsidian's Dataview `TABLE` queries or org-mode column view,
this is the same idea, kept deliberately tiny.

This page carries a live demo below — the tables you see are computed from this help site's own
pages at build time.

rating:: 9

Back to [[track]].

## The grammar

A query is a single expression:

```text
TABLE <column>[, <column>...]
  [FROM #tag]
  [WHERE <cond> [AND <cond>]...]
  [SORT <key> [DESC]]
  [LIMIT <n>]
```

- **Columns and keys** come from two separate namespaces. A **bare** identifier is a note attribute:
  `title` (rendered as a link to the note) and `tags`. A **user [[Properties|property]]** (a sidecar
  value or an inline field) is written `props.<key>` — `props.status`, `props.rating` — and that is
  the *only* way to reach one. Keeping properties under `props.` means a bare word never silently
  picks up a property, and new note attributes can be added later without ever colliding with your
  property names. A multi-valued property fills its cell with `a, b`; a `props.<key>` column shows
  just `<key>` in the table header.
- **An unknown bare key is an error, not an empty column.** `TABLE status` fails with
  `unknown key "status": note attributes are title, tags; query a property as props.status` — so a
  mistyped or mis-namespaced key is caught loudly instead of quietly returning nothing.
- **`FROM #tag`** keeps only notes carrying the tag — or any descendant, since tags are
  hierarchical: `#a` matches `#a` and `#a/b`, never `#ab`.
- **`WHERE`** conditions are `AND`-combined. Each one is a `#tag` filter, a comparison
  (`key = value`, `key != value`, `key < value`, `key > value`), or a bare `key` (the note has the
  property). Values compare by type: numbers numerically, everything else as text — which orders
  `YYYY-MM-DD` dates chronologically. Quote a value with spaces: `owner = "Ada Lovelace"`.
- **`SORT key [DESC]`** orders by a key's first value; notes without the key sort last. Without
  `SORT`, notes come most recently updated first.
- **`LIMIT n`** caps the row count.

Keywords are uppercase, so lowercase words are always keys or values.

```text
TABLE title, props.status, props.due FROM #project WHERE props.status != done AND props.due < 2027-01-01 SORT props.due LIMIT 10
```

## From the command line

`track query` prints the result as JSON, ready for scripts and editor integrations:

```sh
track query 'TABLE title, props.status FROM #project WHERE props.status = open SORT title'
```

```json
{"columns":["title","props.status"],"rows":[{"note_id":1781310000000,"title":"Alpha","cells":["Alpha","open"]}],"count":1}
```

## Embedded query blocks

Fence a block with `track-query` and write the expression inside — the same way a `mermaid` fence
embeds a [[Diagrams|diagram]]. The block renders as a live result table wherever the note renders:
the web workspace and the published static site alike. This block's rows are this site's own
visualization pages — note how `#help/visualization` also matches the nested
`#help/visualization/charts` tag, because tags are hierarchical:

````markdown
```track-query
TABLE title, tags FROM #help/visualization SORT title
```
````

It renders as (live):

```track-query
TABLE title, tags FROM #help/visualization SORT title
```

Property comparisons work the same way, written under `props.`. Two pages of this site carry a
`rating` inline field (this page is one of them — see the strip at the top), so this block finds
them — note the header still reads `rating`, not `props.rating`:

````markdown
```track-query
TABLE title, props.rating WHERE props.rating > 5 SORT props.rating DESC
```
````

It renders as (live):

```track-query
TABLE title, props.rating WHERE props.rating > 5 SORT props.rating DESC
```

In the live workspace the table recomputes as the vault changes; the static export bakes the result
in at build time, over the published notes only — a published query never leaks an unpublished
note. A typo shows an inline error with the expression at the block position, so it never hides
your text.

## Layouts

A block renders as a table by default. A `:layout` header argument on the fence — the same
Org-style header arguments [[Babel]] blocks use — picks a different shape: open the fence with
`track-query :layout board :by status` instead of plain `track-query`, and the same expression
renders as a board instead of a table.

- **`:layout table`** (the default) — the Markdown table above.
- **`:layout board`** — a kanban-style board: one lane per value of the grouping column, cards in
  the query's order.
- **`:layout gallery`** — a grid of cards showing each note's cover image (the note metadata's
  image on a vault; the page's `image:` entry in the site's `site.yml` on a directory site like
  this one). A note without one shows its icon as the card face; with no icon either, track's
  built-in no-image face.
- **`:layout calendar`** — rows placed on a month grid by a date-valued column; one grid per month
  that has rows.

`:by <column>` names the grouping column (board) or the date column (calendar) and must be one of
the `TABLE` columns, written exactly as in `TABLE` — so a property column is `:by props.status`, not
`:by status`; it defaults to the first non-title column. Every layout is read-only — cards and day
entries link to their notes, like a table's title cells. If you know Obsidian's Bases views or
org-mode's agenda, this is the same shape of idea.

Each sample below shows the block's source, then its live result, computed from this help site's
pages.

### Board

Several pages of this site carry a `section` property, given in the site's `site.yml` (the sidecar's
props on a vault); the board lanes them by it. Because `section` is a user property, it is written
`props.section` — bare `section` would be an unknown-key error (the board lane header still reads
`section`):

````markdown
```track-query :layout board :by props.section
TABLE title, props.section, props.rating WHERE props.section SORT title
```
````

It renders as (live):

```track-query :layout board :by props.section
TABLE title, props.section, props.rating WHERE props.section SORT title
```

### Gallery

The visualization pages each carry a cover image and draw it as the card face; the other pages —
like most notes in a real vault — set none, and fall back to their icon. [[Syntax]] and [[Query]]
set no icon either, so their cards show all three states side by side: cover, icon, and track's
built-in no-image face:

````markdown
```track-query :layout gallery
TABLE title FROM #help SORT title
```
````

It renders as (live):

```track-query :layout gallery
TABLE title FROM #help SORT title
```

### Calendar

Some pages carry a `reviewed` date property; the calendar places them on their day, one grid per
month that has rows. Left unbounded, that stacks a grid for every month your dates span — so bound
the range in `WHERE`, where date values compare chronologically. This block keeps June 2026 only:
two of this site's five reviews fall outside it and stay off the grid (`reviewed` is a user
property, so it is written `props.reviewed`):

````markdown
```track-query :layout calendar :by props.reviewed
TABLE title, props.reviewed WHERE props.reviewed > 2026-05-31 AND props.reviewed < 2026-07-01
```
````

It renders as (live):

```track-query :layout calendar :by props.reviewed
TABLE title, props.reviewed WHERE props.reviewed > 2026-05-31 AND props.reviewed < 2026-07-01
```

## Saved queries

Name queries you reuse in `config.yml`. Quote the expression — an unquoted `#` starts a YAML
comment and would silently truncate it:

```yaml
queries:
  open-projects: "TABLE title, props.status, props.due FROM #project WHERE props.status != done SORT props.due"
```

Run one by name with `track query --saved open-projects`, or reference it from a block whose body
is a single `saved:` line — it renders that saved query's table, and takes `:layout` header
arguments like any other block:

````markdown
```track-query :layout board :by props.status
saved: open-projects
```
````

Saved queries come from the vault config, so they are not available to a directory export
(`track export-site --src`, like this help site) — the sample above stays source-only here; write
the expression in the fence there.

## Tags: hierarchy and tag pages

Tags nest with `/` — `#help/visualization/charts` is a `charts` tag under `help/visualization` —
and every tag filter (search `#tag` queries, `FROM`, `WHERE #tag`) matches a tag or any of its
descendants by prefix.

Every rendered tag links to its **tag page**, `/tags/<tag>`, which lists the notes carrying that
tag or a descendant. The [[Web workspace]] serves tag pages live; the static export publishes a
real page per used tag (ancestors included) — the tag beside this page's title is a working example.
On a vault, tags come from note metadata (`track new --tag`, `track append --tag`); on a directory
export like this site, the site's own `site.yml` supplies them, in the page's `pages:` entry — a
page's tags are note-level metadata and are never written in its body.

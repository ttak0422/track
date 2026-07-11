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

- **Columns and keys** are property keys, plus two pseudo-keys: `title` (rendered as a link to the
  note) and `tags`. A multi-valued property fills its cell with `a, b`.
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
TABLE title, status, due FROM #project WHERE status != done AND due < 2027-01-01 SORT due LIMIT 10
```

## From the command line

`track query` prints the result as JSON, ready for scripts and editor integrations:

```sh
track query 'TABLE title, status FROM #project WHERE status = open SORT title'
```

```json
{"columns":["title","status"],"rows":[{"note_id":1781310000000,"title":"Alpha","cells":["Alpha","open"]}],"count":1}
```

## Embedded query blocks

Fence a block with `track-query` and write the expression inside — the same way a `mermaid` fence
embeds a [[Diagrams|diagram]]. The block renders as a live result table wherever the note renders:
the web workspace and the published static site alike. This block contains exactly
`TABLE title, tags FROM #help/visualization SORT title`, and its rows are this site's own
visualization pages — note how `#help/visualization` also matches the nested
`#help/visualization/charts` tag, because tags are hierarchical:

```track-query
TABLE title, tags FROM #help/visualization SORT title
```

Property comparisons work the same way. Two pages of this site carry a `rating` inline field (this
page is one of them — see the strip at the top), so this block finds them:

```track-query
TABLE title, rating WHERE rating > 5 SORT rating DESC
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
  image on a vault; a `cover:: assets/<file>` inline field on a directory site like this one).
- **`:layout calendar`** — rows placed on a month grid by a date-valued column; one grid per month
  that has rows.

`:by <column>` names the grouping column (board) or the date column (calendar) and must be one of
the `TABLE` columns; it defaults to the first non-title column. Every layout is read-only — cards
and day entries link to their notes, like a table's title cells. If you know Obsidian's Bases views
or org-mode's agenda, this is the same shape of idea.

All three samples below are live, computed from this help site's pages.

### Board

Several pages of this site carry a `section::` inline field; the board lanes them by it. This block
is `TABLE title, section, rating WHERE section SORT title` with `:layout board :by section`:

```track-query :layout board :by section
TABLE title, section, rating WHERE section SORT title
```

### Gallery

The visualization pages each carry a cover image; the gallery shows them as cards. This block is
`TABLE title FROM #help/visualization SORT title` with `:layout gallery`:

```track-query :layout gallery
TABLE title FROM #help/visualization SORT title
```

### Calendar

Some pages carry a `reviewed::` date; the calendar places them on their day, one grid per month.
This block is `TABLE title, reviewed WHERE reviewed` with `:layout calendar :by reviewed`:

```track-query :layout calendar :by reviewed
TABLE title, reviewed WHERE reviewed
```

## Saved queries

Name queries you reuse in `config.yml`. Quote the expression — an unquoted `#` starts a YAML
comment and would silently truncate it:

```yaml
queries:
  open-projects: "TABLE title, status, due FROM #project WHERE status != done SORT due"
```

Run one by name with `track query --saved open-projects`, or reference it from a block: a
`track-query` fence whose body is the single line `saved: open-projects` renders that saved query's
table.

Saved queries come from the vault config, so they are not available to a directory export
(`track export-site --src`, like this help site) — write the expression in the fence there.

## Tags: hierarchy and tag pages

Tags nest with `/` — `#help/visualization/charts` is a `charts` tag under `help/visualization` —
and every tag filter (search `#tag` queries, `FROM`, `WHERE #tag`) matches a tag or any of its
descendants by prefix.

Every rendered tag links to its **tag page**, `/tags/<tag>`, which lists the notes carrying that
tag or a descendant. The [[Web workspace]] serves tag pages live; the static export publishes a
real page per used tag (ancestors included) — the tag under this paragraph is a working example.
On a vault, tags come from note metadata (`track new --tag`, `track append --tag`); on a directory
export like this site, a `tags:: a, b` inline field supplies the page's tags.

tags:: help/reference
section:: reference
reviewed:: 2026-07-10

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
the web workspace and the published static site alike. This block contains exactly
`TABLE title, tags FROM #help/visualization SORT title`, and its rows are this site's own
visualization pages — note how `#help/visualization` also matches the nested
`#help/visualization/charts` tag, because tags are hierarchical:

```track-query
TABLE title, tags FROM #help/visualization SORT title
```

Property comparisons work the same way, written under `props.`. Two pages of this site carry a
`rating` inline field (this page is one of them — see the strip at the top), so this block finds
them — note the header still reads `rating`, not `props.rating`:

```track-query
TABLE title, props.rating WHERE props.rating > 5 SORT props.rating DESC
```

In the live workspace the table recomputes as the vault changes; the static export bakes the result
in at build time, over the published notes only — a published query never leaks an unpublished
note. A typo shows an inline error with the expression at the block position, so it never hides
your text.

## Saved queries

Name queries you reuse in `config.yml`. Quote the expression — an unquoted `#` starts a YAML
comment and would silently truncate it:

```yaml
queries:
  open-projects: "TABLE title, props.status, props.due FROM #project WHERE props.status != done SORT props.due"
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

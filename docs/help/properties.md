# Properties

Properties are typed key-value metadata on a note: a status, a rating, a due date, an owner. They
stay out of your prose — either in the note's metadata sidecar or as small `key:: value` fields —
and the engine indexes them with the rest of the vault, so they are queryable and visible wherever
the note is shown. If you know Obsidian's properties or org-mode's property drawers, this is the
same idea.

This page is its own demo: it carries the inline fields below, so the property strip at the top of
this page is rendered from them.

status:: example
rating:: 8
updated:: 2026-07-11
related:: [[Linking notes]]

up:: [[track]]

## Where properties live

track keeps note metadata (title, tags, description) in a per-note sidecar file, not in YAML
frontmatter — the body stays plain Markdown. Properties follow the same rule: they are stored under
`props` in the sidecar and edited through the CLI:

```sh
track meta --title "My note" --set status=draft --set rating=8
track meta --title "My note" --set "authors=[[Ada Lovelace]], [[Alan Turing]]"
track meta --title "My note" --unset rating
track meta --title "My note"          # prints metadata incl. props (JSON)
```

A comma-separated value becomes a list. `--unset` removes a key, and a plain `track meta` call
prints the current properties along with the rest of the note's metadata.

## Inline fields

You can also write a property directly in the body, anywhere prose flows, as `key:: value`:

```text
status:: draft
- rating:: 8
Met with [owner:: [[Ada Lovelace]]] about the plan.
```

Three placements work:

- **A whole line** — `status:: draft` on its own line.
- **A list item** — `- rating:: 8`, numbered items included.
- **Bracketed, mid-sentence** — `[owner:: [[Ada Lovelace]]]` inside a paragraph.

Inline fields are scanned at index time into the same property index as sidecar values, each with
the body line it came from. The text itself still renders as ordinary Markdown — a field is data
*and* prose at once. Code is never scanned: `std::vector` in a fenced block stays code, and a
`[key:: value]` example in inline code (like the ones on this page) never becomes data.

For example, this very sentence carries a live bracketed field, [demo:: [[CLI]]], and that is why
`demo` appears in this page's property strip above — as a link, because its value is a wiki link.

## Typing rules

A value's type is detected from its text, with the same rules everywhere (sidecar, inline, CLI):

| Value text | Type |
| --- | --- |
| `true`, `false` | boolean |
| `8`, `-3.5` | number |
| `2026-07-11` (a real calendar date) | date |
| `[[Title]]` | link |
| `go, lua` (top-level commas) | list — each item typed on its own |
| anything else | string |

A link value keeps its resolution key (`[[Ada Lovelace|Ada]]` stores `Ada Lovelace`), so it resolves
and navigates like any other wiki link — you can see that in the `related` entry of this page's
strip. Commas inside `[[...]]` do not split a list.

## Schema and completion

Optionally, declare a schema for the keys you care about in `config.yml`:

```yaml
properties:
  status:
    type: string
    values: [draft, review, done]
  rating:
    type: number
  due:
    type: date
```

- `type` is one of `string`, `number`, `boolean`, `date`, `link`; `values` is an optional enum.
  Keys not listed stay unconstrained.
- `track doctor` reports every value that breaks the schema — wrong type, or a value outside the
  enum — with the note and body line it came from.
- The editor LSP completes declared keys while you type a field, and completes `values` (or
  `true`/`false` for a boolean) after `key:: `.

## Where properties show up

- The [[Web workspace]] note view (and this published site) shows a note's properties read-only
  above the body — sidecar values first, then inline fields in body order.
- `track meta` prints them as JSON for scripts.
- The index stores every value typed and with line provenance, ready for filtering and sorting in
  future queries.

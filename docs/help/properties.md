# Properties

Properties are typed key-value metadata on a note: a status, a rating, a due date, an owner. Their
home is the note's metadata sidecar, next to its tags and description — out of your prose — and the
engine indexes them with the rest of the vault, so they are queryable and visible wherever the note
is shown. When a data point belongs *in* the prose, a small inline `key:: value` field embeds it
there. If you know Obsidian's properties or org-mode's property drawers, this is the same idea.

This page is its own demo: it carries the inline fields below, so the property strip at the top of
this page is rendered from them.

icon:: 🏷️
status:: example
rating:: 8
updated:: 2026-07-11
related:: [[Linking notes]]

Back to [[track]].

## Where properties live

track keeps note metadata (title, tags, description) in a per-note sidecar file, not in YAML
frontmatter — the body stays plain Markdown. Properties follow the same rule: note-level properties
live under `props` in the sidecar, and every frontend edits them there through the same validated
engine path:

- **The metadata editor** edits the note's whole editable metadata: title, tags, description, cover
  image, and props. The [[Web workspace]] Meta dialog gives each built-in field a dedicated control
  — a title box (a rename on change), a tags box, a description box, and a cover image you can upload
  straight from the browser into the vault assets — and keeps props as the one free-form YAML block.
  The Neovim `:Track meta` popup edits the same metadata as one YAML document. Either way the engine
  validates and applies the edit atomically, and a changed title renames the note.
- **The CLI**, for scripts and point edits:

```sh
track meta --title "My note" --set status=draft --set rating=8
track meta --title "My note" --set "authors=[[Ada Lovelace]], [[Alan Turing]]"
track meta --title "My note" --unset rating
track meta --title "My note"          # prints metadata incl. props and the editable doc (JSON)
track meta --title "My note" --edit - # apply a full metadata document from stdin
```

A comma-separated value becomes a list. `--unset` removes a key, and a plain `track meta` call
prints the current properties along with the rest of the note's metadata.

## Inline fields

The sidecar is the home for note-level facts; when a data point belongs in the prose itself, write
it directly in the body, anywhere prose flows, as `key:: value`:

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

- The [[Web workspace]] note view (and this published site) shows a note's properties above the
  body — sidecar values first, then inline fields in body order. In the live workspace the Meta
  dialog edits the sidecar values; the published site stays read-only.
- `track meta` prints them as JSON for scripts.
- The index stores every value typed and with line provenance, ready for filtering and sorting in
  future queries.

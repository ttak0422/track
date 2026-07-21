# Link Specification

Links are explicit references from note text to other notes, written with `[[...]]`.
Earlier versions linked implicitly by matching every registered title anywhere in the text; that is superseded by this spec (see ADR 0008).

## Syntax

A link is `[[text]]` on a single line.
The inner `text` is the resolution key; surrounding ASCII whitespace is trimmed, so `[[ リンク ]]` is equivalent to `[[リンク]]`.

- The inner text may not contain `[` or `]`, so `[[a]b]]` is not a link.
- Empty or whitespace-only inner text (`[[]]`, `[[ ]]`) is not a link.
- Links do not span lines.
- `[[target|display]]` links to `target` while showing `display` (Obsidian-style). The first `|` separates them; later `|` stay in the display. An empty `display` falls back to `target`, and an empty `target` (`[[|x]]`) is not a link.

### Heading anchors

A target may carry a heading anchor: `[[note#heading]]` links to a heading inside `note`. The number of `#` selects the **Markdown heading level**, and the text after the `#` run is the heading text:

- `[[note#foo]]` targets the first `# foo` (h1).
- `[[note##bar]]` targets the first `## bar` (h2).
- `[[note###baz]]` targets the first `### baz` (h3).

Levels run from h1 (`#`) through **h6 (`######`)**, matching Markdown's ATX heading range. A run of seven or more `#` has no corresponding heading level, so the anchor resolves to nothing: navigation falls back to the note top and completion offers no candidates. The grammar does not clamp extra `#` down to h6.

The note key is the text before the first `#`; it still resolves against titles by exact match. Whitespace around both the key and the heading is trimmed, so `[[ note ## bar ]]` equals `[[note##bar]]`. Anchors compose with display aliases: `[[note##bar|ラベル]]` links to `note`'s `## bar` while showing `ラベル`.

Heading text is **not unique** within a note (a note may have two `## bar` sections). Resolution adopts the **first matching heading** by document order, matching both the level and the text exactly. ATX heading lines are recognized after trimming leading whitespace; a closing `#` run (`## bar ##`) is ignored. Fenced code blocks are skipped, so a `## foo` inside a code fence is not an anchor target.

A `#` with no heading text after it stays part of the note key, so a note titled `C#` is still reachable as `[[C#]]`. A target that is only an anchor with no note key (`[[#foo]]`) is not a link.

When the note resolves but the heading is not found, navigation falls back to the top of the note rather than failing.

Fenced code blocks delimited by lines starting with ` ``` ` are excluded.

### Block anchors

A target may instead carry a block anchor: `[[note#^id]]` links to the block marked `^id` inside
`note`. A block marker is a trailing `^id` — whitespace-separated, so `foo^2` stays prose — at the
end of a non-blank content line outside fenced code; a marker alone on a line is not a marker
(track has no detached "marker under the block" form). Ids are a letter or digit followed by
letters, digits, `-`, or `_`, and are **manual only**: track never generates one (explicit-link
philosophy). Text after `#` that starts with `^` but does not fit the id grammar parses as an
ordinary heading anchor.

The marked block is, for a list item line, the item plus its more-indented continuation lines;
otherwise the contiguous run of non-blank lines around the marker line. The first matching marker
wins when an id repeats, mirroring heading resolution. Like a heading anchor, a block anchor
refines where navigation lands but not which note the link points to, and navigation falls back to
the note top when the id is not found. Renderers hide the marker and anchor the block
(`id="block-<id>"` on the web), so a published page deep-links to it.

### Includes (transclusion)

A line that is **exactly** a link prefixed with `!`, plus optional trailing options, embeds the
target note's content at that position (ADR 0031):

```markdown
![[Note]]
![[Note##設計]] :only-contents
![[議事録|今週の抜粋]] :lines 1-20
```

- Includes are **block-level only**: the `![[...]]` must start the line (leading whitespace
  allowed). An `![[...]]` inside running text is not a directive; its `[[...]]` part is still an
  ordinary link.
- The link part shares the full `[[...]]` grammar — resolution key, heading anchors, `|display`
  alias — and is also a plain link: it resolves the same way, appears in the link graph and
  backlinks, is rewritten on rename, and gets the unresolved-link diagnostic when the key does not
  match. The display alias serves as the embed's caption.
- Without an anchor the whole note body is embedded. With `##heading` the embedded region runs from
  the matched heading line through the line before the next heading of the same or a shallower
  level (headings inside fenced code blocks neither match nor terminate, as above). With `#^id` the
  embedded region is the marked block, its `^id` marker stripped. A non-matching
  anchor is an error surface — the include must render as unresolved, **not** fall back to the
  whole note (unlike navigation).
- Options after the closing `]]` use Org-style `:key value` header arguments, the same shape as
  babel blocks:
  - `:only-contents` — drop the matched heading line and embed only its body. Without an anchor it
    is a no-op.
  - `:lines 4-5,8` — 1-based inclusive ranges over the extracted region (after `:only-contents`),
    concatenated in the order written; out-of-range parts are clipped. Same range syntax as babel's
    `:visible-lines`.
  - Unknown keys and malformed values are collected for diagnostics rather than silently ignored.
- Leading and trailing blank lines of the extracted region are trimmed.
- Embedded content is **not** recursively expanded: an include line inside the embedded region
  renders as text. This bounds the work and makes include cycles harmless by construction.

Extraction lives in the engine (`link.Includes`, `link.Extract`); every surface (Neovim, web, static
export) renders from the same extractor. How each surface displays the embed (virtual lines, card,
depth limits) is that surface's presentation choice.

## Resolution

The target (the inner text before any `|`) resolves against the keyword dictionary by **exact match**:

- each non-empty note `title`

Resolution is an O(1) dictionary lookup, independent of the number of notes. Extraction of `[[...]]` from a line is O(line length), so detection no longer scans the whole body against every keyword.

Titles are kept unique on creation (ADR 0010); if a duplicate ever slips in, the first match by note id wins (`store.ResolveTerm` uses `LIMIT 1`).

Self-links are excluded: a note's own title does not link to itself, and is not offered when completing inside that note.

A `[[...]]` whose inner text matches no title is **unresolved**. It is not written to the link graph and not returned as a document link; the server flags it with a warning diagnostic (see below).

## Link Graph

The Go indexer extracts each note body's `[[...]]` references and resolves them to outgoing links.
Self-links are ignored when writing the graph.
The graph is note-to-note: a heading anchor refines where navigation lands but does not change which note a link points to, so `[[note#foo]]` and `[[note##bar]]` both contribute a single edge to `note`.

`reindex --full` recomputes the complete graph.
Single-note indexing updates only that note's outgoing links, so callers that need newly created inbound links should run a full reindex.

The graph is exposed two ways over the store: the one-hop *local* graph around a single note (`store.LocalGraph`, `:Track graph`, and the web `Local` view) and the whole-vault *global* graph (`store.FullGraph`, the web `Global` view). See [web.md](web.md) for the web rendering.

## Scope

Markdown is a common format, so an editor may attach `track-lsp` to files that are not track notes (this repo's own README, docs, scratch files elsewhere). The server therefore gates every link feature on note membership: a request is served only when the document is a file with a supported extension (`.md`) located inside the configured vault, excluding the track-owned `.track/` directory.

- Notes under `note/` and `journal/` are in scope.
- Anything outside the vault, or under a hidden directory such as `.track/`, gets an empty result: no document links, definition, references, completion, or code actions. `didSave` does not reindex it either.

This is a server-side guarantee that does not depend on the editor. Editors should still avoid attaching the server to non-note buffers where they can (see Neovim Behavior); the server gate is the backstop, not the only line of defense.

## Markdown Action Links

Markdown links whose destination is an angle-bracketed track action are interpreted by the Neovim `:Track follow` path.
They remain ordinary Markdown links for other tools.

Examples:

```markdown
[今日のjournal](<journal?offset=0>)
[昨日のjournal](<journal?offset=-1>)
[今日の会議ノート](<note?template=meeting&title={{date}} Project MTG>)
```

Example meeting note:

```markdown
# Project MTG

[今日の会議ノート](<note?template=project-mtg&title={{date}} Project MTG>)
```

Example template:

```markdown
<!-- track-template
name: project-mtg
-->
# {{ title }}

date: {{ date }}

## Agenda

## Notes

## Actions
```

Following the link on 2026-06-06 creates or opens a regular note titled `20260606 Project MTG` from the `project-mtg` template. The action-link `{{date}}` placeholder uses `yyyyMMdd`; the template body's `{{ date }}` substitution is separate and still uses the configured track date format such as `2026-06-06`.

Current actions:

- `<journal?offset=<n>&template=<name>>`: open or create the journal note at day offset `n`. `offset` is required; `template` is optional and used only when creating.
- `<note?title=<title>&template=<name>>`: open or create a regular note by title. `title` is required; `template` is optional and used only when creating.

Query values are URL-decoded, but percent encoding is not required for spaces because action links always use Markdown's angle-bracket destination form: `[label](<note?title={{date}} Project MTG>)`.
`title` can use `{{date}}` and `{{journal}}` placeholders, both formatted as `yyyyMMdd` and evaluated on the client before calling the CLI.
Track action links cannot run shell commands; executable behavior belongs in templates and will require template trust when implemented.

LSP completion helps build action links:

- after `[label](<`, it offers action snippets such as `note?title={{date}} $0>)` and `journal?offset=0>)`, so accepting the completion closes the Markdown link;
- after `?` or `&`, it offers valid parameter keys for the selected action;
- after `template=`, it offers template names found under `template/`;
- after `title=`, it offers `{{date}}` and `{{journal}}`.

## Neovim Behavior

The Neovim frontend starts `track-lsp` and is the only link frontend.

- It attaches `track-lsp` only to markdown buffers whose file lives under the vault, so unrelated markdown never starts a client. Other editor integrations should gate attachment the same way.
- `textDocument/documentLink` returns ranges over the inner text of **resolved** `[[...]]`, rendered with the `TrackLink` group (linked to `Underlined` by default).
- Unresolved `[[...]]` are left unstyled; the server publishes an `unresolved-link` Warning diagnostic for each, so notes that don't exist yet surface through the editor's diagnostics rather than a separate highlight.
- By default the `[[ ]]` brackets are concealed (and the `target|` of a display alias hidden), so `[[Go]]` shows `Go` and `[[Go|ゴー]]` shows `ゴー`, both underlined. In normal mode the link **under the cursor** is shown raw (anti-conceal) while other links — including others on the same line — stay concealed. While inserting, the whole cursor line is shown raw so byte and screen columns line up and the completion popup stays aligned. Set `conceal = false` to keep brackets visible. Raising conceallevel also lets Neovim's bundled treesitter markdown query hide code-fence delimiters (```lang), so track reveals those fences again by default; toggle with `reveal_code_fences`.
- `:Track follow` and the `<CR>` mapping first handle Markdown action links, then fall back to `textDocument/definition` for `[[...]]` links. With a heading anchor (`[[note##bar]]`) definition jumps to the matching heading line, falling back to the note top when the heading is absent. A same-note anchor (`[[self#foo]]` inside `self`) navigates within the buffer even though a plain self-link does not.
- `textDocument/rename` on a `[[link]]` (or in the current note body when not on a link) renames the target note: it updates the sidecar title, records `.track/renames.yaml`, reindexes, and returns a workspace edit for every `[[oldTitle]]` backlink — including the key of `[[oldTitle|display]]` and `[[oldTitle##anchor]]`. Renaming to the same name, or on an unresolved link, is a no-op. The target note body is not edited.
- `:Track graph` renders a local one-hop graph around the current note using the SQLite link cache: incoming backlinks on the left, the current note in the center, and outgoing links on the right. The graph is a scratch buffer and `<CR>` opens the node under the cursor line.
- `textDocument/completion` offers titles (triggered on `[`) while the cursor is inside an open `[[`, excluding the current note's own terms. While typing the note name (before any `#`), each prefix-matching note also contributes its headings as full `note##heading` anchor candidates next to the bare note name, so reaching a section needs no separate `#` step: `[[te` can offer both `test` and `test##foo`. Because a duplicate heading resolves only to its first occurrence, each `note##heading` (and each `#`-stage heading) is offered once even when the note repeats it. Once the typed target contains `#` (also a trigger character), completion switches to the resolved note's headings whose level equals the typed `#` count — `[[note#` offers h1 headings, `[[note##` offers h2 — inserting the full `note##heading` anchor and showing just the heading text. Body H1 headings are normal anchor candidates. This is a standard LSP capability and is UI-independent: the plugin merges `cmp-nvim-lsp` capabilities when nvim-cmp is installed, so completion surfaces through the user's nvim-cmp setup. Without nvim-cmp, the server still advertises completion for any other client.

# Link Specification

Links are explicit references from note text to other notes, written with `[[...]]`.
Earlier versions linked implicitly by matching every registered title/alias anywhere in the text; that is superseded by this spec (see ADR 0008).

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

The note key is the text before the first `#`; it still resolves against titles and aliases by exact match. Whitespace around both the key and the heading is trimmed, so `[[ note ## bar ]]` equals `[[note##bar]]`. Anchors compose with display aliases: `[[note##bar|ラベル]]` links to `note`'s `## bar` while showing `ラベル`.

Heading text is **not unique** within a note (a note may have two `## bar` sections). Resolution adopts the **first matching heading** by document order, matching both the level and the text exactly. ATX heading lines are recognized after trimming leading whitespace; a closing `#` run (`## bar ##`) is ignored. Fenced code blocks are skipped, so a `## foo` inside a code fence is not an anchor target.

A `#` with no heading text after it stays part of the note key, so a note titled `C#` is still reachable as `[[C#]]`. A target that is only an anchor with no note key (`[[#foo]]`) is not a link.

When the note resolves but the heading is not found, navigation falls back to the top of the note rather than failing.

Fenced code blocks delimited by lines starting with ` ``` ` are excluded.

## Resolution

The target (the inner text before any `|`) resolves against the keyword dictionary by **exact match**:

- each non-empty note `title`
- each non-empty note `alias`

Resolution is an O(1) dictionary lookup, independent of the number of notes. Extraction of `[[...]]` from a line is O(line length), so detection no longer scans the whole body against every keyword.

When a term is ambiguous (e.g. a title and an alias share text), the first match by note id wins (`store.ResolveTerm` uses `LIMIT 1`).

Self-links are excluded: a note's own title or alias does not link to itself, and is not offered when completing inside that note.

A `[[...]]` whose inner text matches no title or alias is **unresolved**. It is not written to the link graph and not returned as a document link; the editor highlights it distinctly (see below).

## Link Graph

The Go indexer extracts each note body's `[[...]]` references and resolves them to outgoing links.
Self-links are ignored when writing the graph.
The graph is note-to-note: a heading anchor refines where navigation lands but does not change which note a link points to, so `[[note#foo]]` and `[[note##bar]]` both contribute a single edge to `note`.

`reindex --full` recomputes the complete graph.
Single-note indexing updates only that note's outgoing links, so callers that need newly created inbound links should run a full reindex.

## Scope

Markdown is a common format, so an editor may attach `track-lsp` to files that are not track notes (this repo's own README, docs, scratch files elsewhere). The server therefore gates every link feature on note membership: a request is served only when the document is a file with a supported extension (`.md`) located inside `$TRACK_VAULT`, excluding the track-owned `.track/` directory.

- Notes directly under the vault and under `journal/` are in scope.
- Anything outside the vault, or under a hidden directory such as `.track/`, gets an empty result: no document links, definition, references, completion, or code actions. `didSave` does not reindex it either.

This is a server-side guarantee that does not depend on the editor. Editors should still avoid attaching the server to non-note buffers where they can (see Neovim Behavior); the server gate is the backstop, not the only line of defense.

## Neovim Behavior

The Neovim frontend starts `track-lsp` and is the only link frontend.

- It attaches `track-lsp` only to markdown buffers whose file lives under the vault, so unrelated markdown never starts a client. Other editor integrations should gate attachment the same way.
- `textDocument/documentLink` returns ranges over the inner text of **resolved** `[[...]]`, rendered with the `TrackLink` group (linked to `Underlined` by default).
- Unresolved `[[...]]` are scanned client-side and rendered with the `TrackLinkUnresolved` group (linked to `Comment` by default), marking notes that don't exist yet.
- By default the `[[ ]]` brackets are concealed (and the `target|` of a display alias hidden), so `[[Go]]` shows `Go` and `[[Go|ゴー]]` shows `ゴー`, both underlined. In normal mode the link **under the cursor** is shown raw (anti-conceal) while other links — including others on the same line — stay concealed. While inserting, the whole cursor line is shown raw so byte and screen columns line up and the completion popup stays aligned. Set `conceal = false` to keep brackets visible. Raising conceallevel also lets Neovim's bundled treesitter markdown query hide code-fence delimiters (```lang), so track reveals those fences again by default; toggle with `reveal_code_fences`.
- `textDocument/definition` (also bound to `<CR>`) jumps from a link to its target note. With a heading anchor (`[[note##bar]]`) it jumps to the matching heading line, falling back to the note top when the heading is absent. A same-note anchor (`[[self#foo]]` inside `self`) navigates within the buffer even though a plain self-link does not.
- `textDocument/completion` offers titles and aliases (triggered on `[`) while the cursor is inside an open `[[`, excluding the current note's own terms. While typing the note name (before any `#`), each prefix-matching note also contributes its headings as full `note##heading` anchor candidates next to the bare note name, so reaching a section needs no separate `#` step: `[[te` can offer both `test` and `test##foo`. This expansion is limited to the note's **title** keyword (one per note); alias-keyed anchors are reached by typing `#`. Once the typed target contains `#` (also a trigger character), completion switches to the resolved note's headings whose level equals the typed `#` count — `[[note#` offers h1 headings, `[[note##` offers h2 — inserting the full `note##heading` anchor and showing just the heading text. In both stages the note's own title heading (its first h1) is omitted, since linking to it is equivalent to linking to the note itself. This is a standard LSP capability and is UI-independent: the plugin merges `cmp-nvim-lsp` capabilities when nvim-cmp is installed, so completion surfaces through the user's nvim-cmp setup. Without nvim-cmp, the server still advertises completion for any other client.

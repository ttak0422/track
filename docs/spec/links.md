# Link Specification

Links are explicit references from note text to other notes, written with `[[...]]`.
Earlier versions linked implicitly by matching every registered title/alias anywhere in the text; that is superseded by this spec (see ADR 0008).

## Syntax

A link is `[[text]]` on a single line.
The inner `text` is the resolution key; surrounding ASCII whitespace is trimmed, so `[[ リンク ]]` is equivalent to `[[リンク]]`.

- The inner text may not contain `[` or `]`, so `[[a]b]]` is not a link.
- Empty or whitespace-only inner text (`[[]]`, `[[ ]]`) is not a link.
- Links do not span lines.
- `[[title|display]]` display aliases are not supported yet.

Fenced code blocks delimited by lines starting with ` ``` ` are excluded.

## Resolution

The inner text resolves against the keyword dictionary by **exact match**:

- each non-empty note `title`
- each non-empty note `alias`

Resolution is an O(1) dictionary lookup, independent of the number of notes. Extraction of `[[...]]` from a line is O(line length), so detection no longer scans the whole body against every keyword.

When a term is ambiguous (e.g. a title and an alias share text), the first match by note id wins (`store.ResolveTerm` uses `LIMIT 1`).

Self-links are excluded: a note's own title or alias does not link to itself, and is not offered when completing inside that note.

A `[[...]]` whose inner text matches no title or alias is **unresolved**. It is not written to the link graph and not returned as a document link; the editor highlights it distinctly (see below).

## Link Graph

The Go indexer extracts each note body's `[[...]]` references and resolves them to outgoing links.
Self-links are ignored when writing the graph.

`reindex --full` recomputes the complete graph.
Single-note indexing updates only that note's outgoing links, so callers that need newly created inbound links should run a full reindex.

## Neovim Behavior

The Neovim frontend starts `track-lsp` and is the only link frontend.

- `textDocument/documentLink` returns ranges over the inner text of **resolved** `[[...]]`, rendered with the `TrackLink` group (linked to `Underlined` by default).
- Unresolved `[[...]]` are scanned client-side and rendered with the `TrackLinkUnresolved` group (linked to `Comment` by default), marking notes that don't exist yet.
- `textDocument/definition` (also bound to `<CR>`) jumps from a link to its target note.
- `textDocument/completion` offers titles and aliases (triggered on `[`) while the cursor is inside an open `[[`, excluding the current note's own terms. This is a standard LSP capability and is UI-independent: the plugin merges `cmp-nvim-lsp` capabilities when nvim-cmp is installed, so completion surfaces through the user's nvim-cmp setup. Without nvim-cmp, the server still advertises completion for any other client.

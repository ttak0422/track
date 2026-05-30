# Auto-Link Specification

Auto-links are implicit links from text to notes.
They are designed to work for Japanese text without requiring explicit markup.

## Keywords

The keyword dictionary is derived from indexed note metadata:

- each non-empty note `title`
- each non-empty note `alias`

Unregistered words do not link.

## Matching Rule

Matching is pure substring matching, not word-boundary matching.
This is intentional because CJK text does not reliably use spaces between words.

When multiple keywords could match at the same byte offset, the longest keyword wins.
Matches are non-overlapping.
Duplicate keyword text keeps the first note id seen by the matcher.

Fenced code blocks delimited by lines starting with ``` are excluded from matching.

## Link Graph

The Go indexer uses the matcher to compute outgoing links for each note body.
Self-links are ignored when writing the graph.

`reindex --full` recomputes the complete graph.
Single-note indexing updates only that note's outgoing links, so callers that need newly created inbound links should run a full reindex.

## Neovim Highlighting

The Lua frontend mirrors the Go matching rule for interactive highlighting.

Only visible lines are scanned, with a debounce, to keep highlighting cheap on large files.
Highlighted ranges are cached so `:TrackFollow` and `<CR>` follow exactly the link under the cursor.

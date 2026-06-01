# ADR 0009: Heading Anchor Links with Hash-Count Levels

## Status

Accepted (extends [ADR 0008](0008-explicit-wiki-links.md))

## Context

`[[...]]` links resolve to a whole note (ADR 0008). Notes grow long enough that landing at the top is not always useful; authors want to point at a specific section. Obsidian writes section links as `[[note#heading]]` and block links as `[[note#^block]]`, treating `#` as a single heading separator regardless of level. track does not target Obsidian compatibility, so it is free to pick a syntax that fits its own model.

The open question was what a run of `#` should mean. The chosen reading aligns the link syntax with the Markdown heading syntax it points at.

## Decision

A link target may carry a heading anchor, and the number of `#` selects the Markdown heading level.

- `[[note#foo]]` targets an h1 `# foo`, `[[note##bar]]` an h2 `## bar`, and so on through h6. The text after the `#` run is the heading text.
- The note key is the text before the first `#` and still resolves against titles and aliases by exact match. Anchors compose with display aliases (`[[note##bar|label]]`) and with whitespace trimming.
- Heading text is **not unique** within a note, and track does not try to make it so. Resolution adopts the **first heading** that matches both level and text exactly, in document order. This keeps resolution cheap and predictable without a uniqueness constraint or disambiguation syntax.
- ATX headings are matched after trimming leading whitespace; a closing `#` run (`## bar ##`) is ignored, and headings inside fenced code blocks are skipped — the same exclusions the title parser already uses.
- A `#` with no heading text stays part of the note key, so a note titled `C#` remains reachable as `[[C#]]`. An anchor with no note key (`[[#foo]]`) is not a link.
- The anchor refines navigation only. The link graph stays note-to-note, so anchors do not change backlinks, references, or edge counts.
- In the editor, `textDocument/definition` jumps to the matched heading line (top-of-note fallback when absent), same-note anchors navigate within the buffer, and completion offers the resolved note's headings once the typed target contains `#`.

## Consequences

Section links read naturally for anyone who already knows Markdown heading levels: the hashes in the link mirror the hashes on the heading. Resolution stays a linear scan of the target note's heading lines with no new index state.

The cost is that a note title containing an internal `#` (e.g. `C#sharp`) is shadowed by the anchor grammar, and there is no escape hatch yet. "First match wins" also means duplicate headings are reachable only at their first occurrence; pointing at a later duplicate would need a disambiguation syntax that is intentionally not specified here. Block-level anchors (Obsidian's `#^`) are out of scope: the design covers headings only.

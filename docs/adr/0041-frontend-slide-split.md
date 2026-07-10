# 0041. Slide decks split in the frontend, addressed by URL hash

Status: Accepted

## Context

Any note should be presentable as a slide deck, with `---` thematic breaks separating slides —
org-mode and Obsidian users expect this shape. The project rule is that the Go engine owns note
semantics, but the engine has no Markdown block parser: bodies pass through export/sanitize as
lines, and all block-level parsing (CommonMark + GFM) happens once, in the web frontend's
react-markdown pipeline. Whether a given `---` line is a thematic break, a setext-heading
underline, or fenced-code content is a block-parsing question.

## Decision

- **The split lives in the frontend** (`web/src/components/slides.ts`), next to the only Markdown
  block parser in the system. Duplicating enough of CommonMark in Go to classify `---` lines would
  create a second parser that can drift from what the page actually renders. The engine, API, and
  static bundle are unchanged; slides are pure presentation.
- **Split rule.** A `---` line (up to 3 leading spaces, `- - -`/`----` variants included) separates
  slides when it sits outside fenced code and directly under a blank line, an ATX heading, or
  another separator — the same conditions under which the page renders it as a horizontal rule
  rather than a setext heading. `***`/`___` rules do not split. Blank slides are dropped.
- **The deck is the note page plus a hash**, not a route. `#slide=N` on the existing note URL opens
  the deck on slide N; keyboard and on-screen controls rewrite the hash in place. This makes decks
  deep-linkable and work identically on the live workspace and the prerendered static site with no
  new routes, no extra prerendered files, and no router involvement.
- Each slide renders through the existing `MarkdownView`; resolved `![[...]]` include line numbers
  are rebased onto the slide's text, so everything a note page shows works inside a slide.

## Consequences

- Other frontends (Neovim) get no slide support from the engine; if one ever needs it, the split
  rule above is the spec to implement — or the split moves into the engine alongside a real block
  parser, not before.
- A `---` under a paragraph line is a setext heading and does not split; docs/help/slides.md
  documents the blank-line requirement.

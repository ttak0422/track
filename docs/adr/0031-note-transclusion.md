# 0031. Note transclusion: `![[...]]` includes with Org-style options

Status: Accepted

## Context

Notes need a Confluence-excerpt-like way to surface another note's content in place — a task note
embedding a design section, a weekly note embedding meeting minutes — without copying text that
then goes stale. The vault already has an explicit link grammar (`[[note##heading|display]]`,
ADR 0008 / docs/spec/links.md), a media-embed convention (`![alt](url)` renders the target instead
of linking it), and babel blocks that carry Org-style `:key value` header arguments.

Prior art: Org's `#+INCLUDE:` (export-time) and org-transclusion (live, read-only display in the
buffer) both reuse the ordinary link syntax to say *what* to embed and trailing `:key value`
properties to say *how* — the selector grammar and the presentation options stay separate layers.

## Decision

- **Notation.** A block-level line `![[target]] :options...` transcludes the target. The `!` prefix
  mirrors the `![alt](url)` embed convention; the link part is the unchanged `[[...]]` grammar, so
  heading anchors select sections (`![[note##設計]]`) and the display alias becomes the embed's
  caption. Options are Org-style header arguments like babel's: `:only-contents`, `:lines 4-5,8`
  (babel's `:visible-lines` range syntax). Unknown options collect into diagnostics.
- **An include is also a link.** The `[[...]]` inside resolves through the same dictionary, joins
  the link graph and backlinks, follows renames, and reuses the unresolved-link diagnostic. No
  second resolution path.
- **The engine owns extraction.** Parsing (`link.Includes`) and region extraction (`link.Extract`)
  live in the link package; Neovim, the live web workspace, and the static export all render from
  this one extractor, so section semantics can never drift per surface.
- **One level, no recursion.** Embedded content is not re-expanded; a nested include renders as
  text. Cycles are harmless by construction. If real nesting demand appears, it becomes an explicit
  depth option later.
- **A missing anchor is an error, not a fallback.** Navigation falls back to the note top; an
  include embedding the whole note because a heading was renamed would silently misinform, so it
  renders as unresolved instead.
- **Presentation is per surface.** Neovim shows read-only virtual lines below the directive
  (org-transclusion's shape) with an editor-side display cap; the web surfaces render an embed
  card. Caps, folding, and styling are surface concerns and need no shared spec.

## Consequences

- The grammar reuses existing machinery end to end (link parsing, anchors, rename rewriting,
  diagnostics, babel option shape); the only new engine code is directive detection and region
  slicing.
- `:lines` is ordinal and can rot as the source note is edited — it is intended for pinning short
  excerpts, and heading anchors remain the primary selector.
- Because includes are backlinks, the embedded note's backlink list shows where it is transcluded,
  for free.
- Future selectors (`:block <name>` for named babel blocks, excerpt markers) extend the option
  grammar without touching the link syntax.

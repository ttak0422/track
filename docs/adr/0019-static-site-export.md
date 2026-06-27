# 0019. Static site export

Status: Accepted

## Context

`track export` writes a single note out as Markdown. The next requested capability is publishing a
chosen set of notes as a browsable, self-contained static site that can be mounted on GitHub Pages
(e.g. to ship usage/help docs alongside the repository). Requirements:

- Rendered HTML content only — no live index, heatmap top page, or preview/edit toggles.
- Links between selected notes are navigable; links to notes outside the selection are inert.
- Referenced assets are published.
- A machine-friendly CLI interface (an explicit set of note ids), since selection is driven by
  automation rather than interactive picking.

The roadmap originally framed batch export as a Markdown bundle first, but the concrete need here is a
ready-to-host HTML site, so we target HTML directly.

## Decision

Add `track export-site --root <id> [--id <id> ...] --out <dir>`, backed by a new store-free package
`internal/track/site`.

- **Selection is an explicit id set.** `--root` designates the entry note, rendered as `index.html`
  (the live heatmap top page is intentionally not published). Each additional `--id` note becomes
  `<id>.html`. The root is always part of the set. Tag/query-based selection is deferred.
- **Two-stage rendering reuses the export pipeline.** The note body is first transformed by
  `internal/track/export` with a site-specific `Renderer` into intermediate Markdown, then
  [goldmark](https://github.com/yuin/goldmark) (GFM extension) converts that Markdown to HTML. This
  keeps track-specific span handling (`[[...]]`, action links, babel blocks) in one place and delegates
  CommonMark/GFM block structure to a maintained library rather than reinventing it.
- **Wiki links resolve against the selection.** A `[[key]]` whose key resolves (via the caller-supplied
  `Resolver`, normally the index) to a note in the set becomes a relative `<a href="<page>.html">`;
  anything outside the set is flattened to inert display text. Output filenames are flat and relative so
  the site works under a GitHub Pages subpath without a `<base>`.
- **The site package is store-free.** It takes a `Resolver` closure and a `*config.Config`; the CLI
  wires the closure to `store.ResolveTerm`. This mirrors the link package's split between extraction
  and resolution and keeps the package unit-testable.

## Consequences

- A new Go dependency (goldmark) is added; the Nix `vendorHash` must be refreshed when it changes.
- Rendering fidelity now has two paths: the live web frontend (react-markdown) and the static export
  (goldmark). They can diverge; track-specific syntax is shared through the export renderer, but
  frontend-only niceties (hover previews, OGP cards, budoux line breaking) are not reproduced in the
  static output by default.
- Asset publishing and mermaid rendering are layered on in follow-up changes; the first slice
  establishes the command, page layout, and link resolution.

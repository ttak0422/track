# 0022. Composed visualization documents (articles)

Status: Accepted

## Context

ADR 0021 established the visualization pipeline: a Canonical Data Model, a renderer-independent View
Spec, and a Chart.js renderer that writes one chart to one HTML file. The visualization goal, however,
is a *data article* — narrative prose interleaved with several charts (a time series with event
markers, a ranking, a sector bubble), produced from one declarative spec. A single chart per file does
not reach that.

Two questions had to be settled:

1. **What is the composition unit?** A standalone chart spec is one chart. We need an ordered document
   of prose and charts.
2. **How is Markdown prose rendered?** ADR 0019 deliberately removed the goldmark dependency; we do not
   want to re-add a Go Markdown engine just for this.

## Decision

Add an **article spec**: a JSON document with a `blocks` array, each block being *exactly one* of
`markdown` (prose) or `chart` (an inline View Spec). It lives in `internal/track/article` as a pure
spec/validation layer, mirroring `viewspec` — no file IO, no renderer.

- **`track render` auto-detects** an article by a top-level `blocks` array; otherwise it renders a
  single chart. One command, one mental model. Chart blocks reuse the exact same data resolution and
  Chart.js config builder as standalone charts (`resolveChart` and `chartJSConfigJSON` are shared), so
  a chart renders identically whether standalone or embedded.
- **Prose is rendered client-side by marked.js from a CDN**, not in Go. This keeps track free of a Go
  Markdown dependency (consistent with ADR 0019) and matches the existing CDN-first approach (Chart.js,
  the annotation plugin). The page ships the Markdown sources as a JSON array and calls `marked.parse`
  at view time.
- **CDNs load conditionally:** Chart.js always; the annotation plugin only when some chart has overlay
  markers; marked only when the document has prose. Plain outputs stay lean.

## Alternatives considered

- **Render Markdown to HTML in Go.** Rejected: it means re-adding a Markdown engine (the dependency
  0019 removed) for a feature that a 10 KB CDN script covers, and the rest of the page is already
  CDN-driven and view-time rendered.
- **A separate `track render-article` command.** Rejected: auto-detection by `blocks` keeps one verb
  and one help surface; the spec shape is unambiguous.
- **Server-side static HTML for charts too.** Out of scope; the whole pipeline is CDN/view-time by
  ADR 0021, and an offline/static path is future work (a second renderer), not specific to articles.

## Consequences

- A new package (`internal/track/article`) and a document renderer (`render.RenderDocument`); the
  single-chart path is refactored to share resolution and config building, so the two cannot drift.
- Composed pages require network access at view time for Chart.js, marked, and (if used) the annotation
  plugin.
- Markdown is inserted as `innerHTML` from the user's own spec; this is a local authoring tool, so it is
  treated as trusted input. Output sanitization is future work if articles are ever published from
  untrusted sources.
- An offline/no-CDN article (e.g. a future SVG renderer plus server-side Markdown) remains open; the
  article spec is renderer-agnostic and would not change.

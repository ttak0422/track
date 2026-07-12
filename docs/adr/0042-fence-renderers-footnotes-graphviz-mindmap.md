# 42. Footnotes, Graphviz, and mindmaps as frontend fence renderers

Date: 2026-07-11

## Status

Accepted. Follows the pattern set by the Mermaid fence renderer and the JS delivery policy (ADR 0029).

## Context

Three rendering additions were wanted in note bodies: GFM footnotes, Graphviz DOT diagrams, and a
mindmap view of a note's structure. The engine's web export (`export.NewWebRenderer`) deliberately
passes ordinary Markdown lines and non-babel fences through verbatim, and the static export is the same
React frontend running over a pre-generated bundle — so the decision is where each renderer lives and
what it depends on.

For Graphviz specifically there were two candidate shapes: render client-side (as Mermaid does), or
shell out to a locally installed `dot` binary at export time and fall back to a code block when absent.

## Decision

All three are frontend renderers keyed off content the engine already delivers untouched; the engine
and bundle format are unchanged.

- **Footnotes** come from `remark-gfm`, which the frontend already ships. The only code is keeping the
  generated reference/back-link anchors' ids intact (the generic link component would drop them) plus
  end-matter styling.
- **Graphviz** renders client-side with `@hpcc-js/wasm-graphviz` (~2 MB, zero transitive deps, WASM
  inlined — no network fetch at view time), lazily imported exactly like Mermaid and sharing the same
  pan/zoom presentation shell (`DiagramFrame`, extracted from `MermaidDiagram`). The export-time `dot`
  binary alternative was rejected: it renders nothing in the live workspace, adds a host dependency the
  static export cannot vendor, and needs a divergent prerender path. Dark mode is a CSS
  invert/hue-rotate filter, not SVG recoloring — Graphviz output has fixed colors.
- **Mindmaps** are an in-house layout (`mindmap.ts`: outline/heading parsing plus a leaf-per-row tidy
  tree) emitted as plain inline SVG React elements. No engine dependency means the static prerender
  contains the finished diagram before any script runs, and theme colors ride the CSS variables.
  An empty ```` ```mindmap ```` fence maps the surrounding note's heading tree, read from a
  markdown-source context the note view provides.

PlantUML stays out of scope: it has no self-contained WASM build, so it would break the
"self-contained static export" property the other renderers keep.

## Consequences

- Rendering additions of this kind follow one recipe: engine passes the fence through, a lazily loaded
  component in `MarkdownView`'s fence dispatch renders it, and both the live workspace and the static
  export get it for free.
- `DiagramFrame` is the shared shell for any future SVG-producing diagram engine (loading, error +
  source fallback, fit/pan/zoom/collapse).
- The Graphviz dark theme is approximate (filter-based); exact theming would require rewriting SVG
  attributes.
- Mindmap label sizing uses a character-width heuristic instead of canvas measurement; very unusual
  fonts may clip, and the upgrade path is `measureText`.

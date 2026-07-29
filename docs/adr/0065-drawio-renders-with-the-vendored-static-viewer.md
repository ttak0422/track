# 0065: draw.io renders with the vendored static viewer

## Status

Accepted (2026-07-30)

## Context

Notes should be able to hold draw.io (diagrams.net) diagrams — fenced ```drawio
blocks and `.drawio` attachments — rendered offline in both the live workspace
and the published static export, like the Mermaid/Graphviz/D2 engines already
are. Unlike those engines, draw.io publishes no npm renderer:

- The `mxgraph` npm package (4.2.2) is archived and deprecated, and ships only
  ~15 basic shapes. Everything drawio users actually draw with — AWS/UML/BPMN
  sets, mockups, callouts — lives in drawio's own shape packs, so real diagrams
  silently degrade to labeled gray rectangles. Its default stylesheet also
  differs from drawio's, so even basic shapes come out with wrong colors.
- `@maxgraph/core` (the maintained successor) can import an `<mxGraphModel>`
  through a compatibility codec, but carries the same core-shapes-only universe
  and does not claim to render .drawio files.
- Embedding `viewer.diagrams.net` in an iframe needs the network, which the
  static export must not.

drawio's own `viewer-static.min.js` build is the renderer the product uses,
with every built-in shape and stylesheet inlined (nothing fetched at runtime)
and both `<diagram>` payload encodings (plain XML and the legacy
base64+deflate) decoded internally. It is Apache-2.0; self-hosting the file is
an upstream-documented pattern. It is, however, not on npm.

## Decision

Vendor `viewer-static.min.js` (pinned: jgraph/drawio v31.1.2) into
`web/public/`, with its provenance and license recorded beside it. A loader
(`drawioViewer.ts`) injects the script lazily on the first drawio render and
blanks the `viewer.diagrams.net` fallback URL globals first, so a published
site never phones diagrams.net; exotic features that would need them (MathJax
labels, external stencil URLs) degrade instead. `DrawioDiagram` hands the raw
`<mxfile>`/`<mxGraphModel>` text to `GraphViewer.createViewerForElement` — no
toolbar, no lightbox, first page only.

## Consequences

- Real-world diagrams render with editor fidelity, offline, at the cost of a
  ~4.1 MB (≈850 KB gzipped) vendored script that only loads when a note shows
  a drawio diagram.
- The file is updated by hand against upstream tags, not by npm; the pinned
  version lives in the license note and `drawioViewer.ts`.
- The viewer owns a live container, so drawio diagrams use their own card
  instead of the shared SVG pan/zoom `DiagramFrame`.
- `.drawio.svg` / `.drawio.png` exports stay ordinary images (they are valid
  images with the source embedded), needing no engine at all.

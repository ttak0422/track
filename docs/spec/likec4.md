# LikeC4 note diagrams

The `likec4` Markdown fence and `.c4` / `.likec4` text attachments describe one self-contained
architecture model with multiple explicit views. The same React renderer is used by the live Web
workspace, the native WKWebView frontend, and the public static site.

## Runtime boundary

- `LikeC4Diagram` observes viewport visibility before dynamically importing the engine. The parser,
  layout code and bundled Graphviz WASM run in an ES module worker, not on the reading UI thread.
- The worker uses the official `@likec4/language-server/browser` entry and `NoFileSystem`. It creates
  one in-memory document in an isolated project. No LSP connection, filesystem watcher, user project
  configuration, remote renderer or icon service is started.
- The language services validate the document before computing its model. Only views with a
  source path are displayed: LikeC4's automatically synthesized landscape is omitted.
  The note must declare at least one view. Generated implicit per-element views are disabled.
- Each computed view is rendered through LikeC4's Graphviz SVG exporter. Views are rendered
  sequentially and every failure is propagated; a partial subset must not look like a complete
  successful model. Icons are removed from the computed drawing rather than reserving blank space.
- `safeDiagramSvg` applies DOMPurify's SVG profile, excludes scripts, styles, foreign HTML, images,
  links, external references and animation, then rejects non-SVG output. This strict profile is for
  the Graphviz output and must not be reused for a CSS/HTML-dependent renderer without review.
- One block allows 100,000 UTF-16 code units, 24 declared views and 500 nodes per view. A 30-second
  parent-side timer terminates a stuck worker. Replacement, unmount, error and successful completion
  also terminate the worker, releasing its parser and WASM memory.

## Reader behavior

View buttons use text controls (design variant 1). Selecting a view remounts the shared
`DiagramFrame`, so initial fit, pan, zoom, Reset and popup behavior stay consistent with other note
diagrams. The full source is available from Copy source and is retained alongside error messages.
The selected view survives source edits when its identifier still exists.

The renderer is the official Graphviz exporter, not the full LikeC4 React application. Some shapes
and layout differ, and icons, custom CSS/HTML, clickable links, node navigation and manual layouts
are intentionally unsupported. Separate blocks do not share declarations or files. Public export
packages the worker and WASM with the frontend and does not pre-render through a server.

The language server package currently requires Node 22.22.3 or newer for building; Node-only
optional configuration dependencies are not required by this browser entry. The production build
must continue resolving browser package conditions without Node polyfills. Browser QA exercises
the real worker/parser/WASM pipeline; unit tests cover worker lifetime, SVG sanitation, view
selection, cancellation and Markdown/attachment dispatch.

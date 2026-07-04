# 0021. Visualization on a Canonical Data Model

Status: Accepted

## Context

track should grow from a note tool into a base for market analysis and visualization: prices, indices,
FX, sector heatmaps, news, social posts, custom indicators, and event timelines —
combined and drawn from a declarative spec, the way Mermaid draws diagrams. (See the "track 可視化 PJ"
project note for the full vision.)

That vision is large, so this ADR fixes the boundaries and the first slice rather than the whole thing.
The central tension is coupling: if track learns about Alpha Vantage, RSS, or a social API, every new
data source changes track. We want the opposite — track unaware of the outside world.

## Decision

**track owns a Canonical Data Model and visualization over it; it never fetches.** External access
(market APIs, RSS, news, social, scraping, manual URL entry) is the job of separate `track-fetch-*`
tools that convert the outside world into canonical records and hand them to track. track's
responsibilities are only: the data model, import/export, query, metric computation, View Spec
interpretation, and rendering. This split is the load-bearing decision — it lets `track-fetch-*` tools
multiply without touching track.

Concretely, for the first slice:

- **Canonical Data Model in `internal/track/dataset`.** Five record kinds — `event`, `price` (OHLCV),
  `metric`, `entity`, `annotation` — documented as typed structs, each carrying a schema `version`.
  **JSONL is the first-class format**, one homogeneous file per kind (`prices.jsonl`, ...). The render
  pipeline reads JSONL generically (`Record = map[string]any`) so a View Spec can address any field by
  name without the model knowing the spec.
- **Storage is JSONL files only — no SQLite for this data.** The note index stays its own thing
  (ADR 0002); visualization data lives in plain JSONL the user points at. A query/index layer can come
  later without changing the model.
- **Renderer-independent View Spec in `internal/track/viewspec`.** A spec names a data source (a JSONL
  file of one kind), maps record fields onto chart encodings (x and one or more y series), and an
  optional equality filter. It knows nothing about Chart.js, SVG, or D3.
- **Pluggable renderers in `internal/track/render`.** A `Renderer` turns a resolved spec into output and
  registers under a name. The MVP ships one: **`chartjs`**, emitting a self-contained HTML page that
  loads **Chart.js from a CDN** and draws line/bar/scatter. SVG/D3 renderers can be added later without
  touching the spec or model.
- **`track render --spec <file> --out <file>`** is the entry point: it resolves a spec against its
  JSONL and **writes the rendered document to a file**. It is independent of the note index/store, so it
  works on any canonical JSONL, in a vault or not.

## Alternatives considered

- **Embed View Specs in notes (Babel code blocks), with live re-render.** The web reader already has the
  machinery for this (fsnotify + SSE; see ADR 0019 / `internal/track/webui`), so note-embedded specs
  could update in place as a note (or its data) is saved. We deferred it: the standalone-spec path is
  simpler, testable without the server, and keeps track's single source of truth in one place. The View
  Spec struct is the same unit a Babel path would parse, so this remains a pure addition later — no model
  change required.
- **Import canonical data into SQLite.** Rejected for the MVP as premature; JSONL files are enough to get
  data → spec → chart working, and the model is storage-agnostic if a query layer is added later.
- **Bundle Chart.js into the output.** Rejected for now in favor of a CDN reference to keep output simple;
  pinning/bundling is a renderer-local change.

## Consequences

- A new top-level CLI verb, `track render`, and three new packages (`dataset`, `viewspec`, `render`).
- Generated pages require network access at view time to load Chart.js from the CDN.
- `track-fetch-*` tools are out of scope here; they are expected as separate binaries that emit canonical
  JSONL. track's surface does not change when they are added.
- The View Spec and renderer registry are intentionally minimal (line/bar/scatter, single equality
  filter). Richer chart types, queries, and a note-embedded live path are future work that this structure
  is meant to absorb without rework.

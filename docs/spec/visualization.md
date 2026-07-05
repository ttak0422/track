# Visualization: Canonical Data Model, View Spec, and Rendering

This document specifies how track represents visualization data and how it draws charts from a
declarative spec. Design rationale is in [ADR 0021](../adr/0021-visualization-canonical-data-model.md)
(Canonical Data Model, renderer registry) and
[ADR 0024](../adr/0024-mark-encoding-view-spec.md) (the mark + encoding View Spec, v2).

track never fetches data. External sources are converted into the Canonical Data Model below by
separate `track-fetch-*` tools (out of scope here); track only imports, queries, and renders that model.

## Canonical Data Model

The model is defined in `internal/track/dataset`. Data is stored as **JSONL** — one JSON object per
line, one homogeneous file per kind (e.g. `prices.jsonl`). Every record carries a schema `version`
(current: `1`) so the format can evolve.

These JSONL files are the source of truth; track keeps no separate data store. The vault's `data/`
directory (created by `track init`) is where `track-fetch-*` tools write their output, so the canonical
data sits alongside the notes that reference it. A View Spec points at a file by path, so data may also
live anywhere a spec can reach.

Blank lines are skipped. A malformed line fails the whole read with its line number; data is never
silently dropped.

### Kinds

| Kind         | Required fields            | Optional fields                  | Meaning                                   |
|--------------|----------------------------|----------------------------------|-------------------------------------------|
| `event`      | `time`, `title`            | `entity`, `url`, `note`          | A point-in-time happening                 |
| `price`      | `entity`, `time`, `open`, `high`, `low`, `close` | `volume`   | One OHLCV bar                             |
| `metric`     | `name`, `time`, `value`    | `entity`                         | A named numeric series sample             |
| `entity`     | `id`, `name`               | `kind` (stock/index/fx/…)        | A thing series refer to                   |
| `annotation` | `time`, `text`             | `target`                         | A label for narrative overlays            |

All records also carry `version`. `time` is an RFC 3339 / date-like string; it is treated as an opaque
category label by the renderer (no time-axis parsing in the MVP).

The kinds above are defined as typed structs in `internal/track/dataset` (the contract that
`track-fetch-*` writers target). The same field schema is derived from those structs (via
`dataset.KindFields`) and printed by `track render --help`, so this table, the help text, and the code
stay in sync from one source.

Example `prices.jsonl`:

```jsonl
{"version":1,"entity":"AAPL","time":"2026-06-01","open":190,"high":195,"low":189,"close":194,"volume":1000}
{"version":1,"entity":"AAPL","time":"2026-06-02","open":194,"high":198,"low":193,"close":197,"volume":1200}
```

Numeric fields are read losslessly (decoded as `json.Number`); numeric strings (`"3.5"`) are also
accepted. The render pipeline reads records generically, so a View Spec may address any **extra** field
present in the data by name, on top of the documented ones.

`kind` is a real schema, not a loose label: rendering validates every record against its kind
(`dataset.Validate`) and **fails with an error rather than drawing a partial chart** if a required
field is missing, a numeric field is non-numeric, or the schema version is newer than supported. Extra
fields are still allowed, so a record may carry custom columns a spec then charts. (Validation is
deliberately strict; loosening a field later is a one-line struct change.)

## View Spec

Defined in `internal/track/viewspec`. A View Spec is a renderer-independent JSON description of one
chart over a single data source. A **`mark`** names *what* is drawn; an **`encoding`** maps record
fields onto *visual channels* (x, y series, color, size), orthogonally. Unknown fields are rejected.

```json
{
  "version": 2,
  "mark": "line",
  "title": "AAPL close",
  "data": { "source": "prices.jsonl", "kind": "price" },
  "encoding": {
    "x": { "field": "time", "title": "Date" },
    "y": [ { "field": "close", "title": "Close" } ]
  },
  "filter": { "field": "entity", "equals": "AAPL" }
}
```

| Field            | Required | Notes                                                                 |
|------------------|----------|-----------------------------------------------------------------------|
| `version`        | yes      | View Spec schema version (current: `2`).                              |
| `mark`           | yes      | `line`, `bar`, `point`, `area`, `rect`, or `candlestick`.             |
| `title`          | no       | Chart and page title.                                                 |
| `data.source`    | one of   | Path to a JSONL file, resolved **relative to the spec file** (or absolute). |
| `data.records`   | one of   | Inline data: an array of records carried in the spec (mutually exclusive with `source`). |
| `data.kind`      | yes      | One of the canonical kinds.                                           |
| `encoding.x`     | yes      | The x channel: `{ field, type?, title? }`.                           |
| `encoding.y`     | yes*     | One or more y series; each `{ field, type?, title?, axis? }`. (*`candlestick` takes none — its OHLC fields are implied.) |
| `encoding.color` | rect     | On `rect`: the (quantitative) heatmap cell value. On every other mark: a **nominal** category that splits records into one colored series per value (see below). |
| `encoding.size`  | no       | Radius channel for a `point` (bubble radius / timeline dot).         |
| `filter`         | no       | Keep matching records. Shorthand `{field, equals}` or `{all: [{field, op, value}]}` (AND). |
| `overlays`       | no       | Reference geometry drawn over the chart: markers, lines, bands (see below). |

A channel's `type` is `quantitative` (default, a measure) or `nominal` (a category). The type is the
hint that lets one mark cover the former chart types, since it names which axis is categorical:

- **`bar`** with a **nominal y** (and the measure on `x`) draws **horizontal** bars (a ranking);
  a nominal x with a numeric y is the usual vertical bar.
- **`point`** is a **scatter** with a nominal x, a **bubble** (linear axes, `{x,y,r}`) with a
  quantitative x, or a **timeline** swimlane with a nominal y.
- **`rect`** is a **heatmap**: nominal x and y form the grid, `color` gives the cell value.
- **`area`** is a line with the region between it and the zero baseline filled (translucently, so
  overlapping series stay readable). It resolves to the same series shape as a line, so every line
  channel — multi-series y, `color` split, `axis: "y2"`, `sort`/`limit` — works unchanged.
- **`candlestick`** draws one OHLC candle per record: a high–low wick behind an open–close body,
  green when the close is at or above the open and red otherwise (a doji keeps a 1px body). It
  requires `data.kind: "price"` and reads the kind's canonical `open`/`high`/`low`/`close` fields
  directly, so the encoding needs only `x` — `encoding.y`, `color`, and `size` are rejected. A candle
  missing any component is skipped (the ECharts renderer draws it as a null gap).

Channels also take per-channel options whose placement is validated (a misplaced option is an error,
not a silent no-op):

- `sort` / `limit` — on the **category-axis channel** only (`x` for line/bar/point; `y[0]` for a
  horizontal bar). See "Sort, top-N, and stacking" below.
- `stack` — on a **bar's measure channel** only (`y[0]`, or `x` for a horizontal bar).

`title` overrides the legend/axis text, defaulting to the field name. A y channel may set
`"axis": "y2"` to plot on a secondary right-hand axis (default `"y"`), so series on different scales —
e.g. a price and an index — can share one x-axis:

```json
"y": [
  { "field": "close", "title": "Close", "axis": "y" },
  { "field": "index", "title": "Index", "axis": "y2" }
]
```

### Color (series split by category)

On every mark except `rect`, `encoding.color` names a **nominal** field whose values split the records
into one series per category — each drawn in its own color and listed in the legend under the category
value. One spec covers "one line per entity", grouped bars per category, or a scatter/bubble colored by
group:

```json
{ "version": 2, "mark": "line", "data": { "source": "metrics.jsonl", "kind": "metric" },
  "encoding": { "x": { "field": "time" },
                "y": [ { "field": "value" } ],
                "color": { "field": "entity", "type": "nominal" } } }
```

- `color` must set `"type": "nominal"` (on `rect` it is instead the quantitative cell value).
- It combines with a **single** `y` channel (each category becomes its own series).
- Categories and x labels accumulate in **first-seen order**, so the same input always produces the
  same series order — and therefore the same colors. Both renderers assign colors from the same
  fixed palette by series index, so a spec is colored identically in HTML and SVG output. The
  heatmap's light→dark value ramp is a separate scale and is unaffected.
- A category with no record at some x label contributes `NaN` there (a gap, not a zero). A repeated
  `(x, category)` pair keeps the later record's value.
- A timeline (`point` with a nominal y) rejects `color`: its lanes are already colored by the
  nominal y.

### Sort, top-N, and stacking

`sort` orders the category axis. It sits on the channel that supplies the category labels — `x` for
line/bar/point, `y[0]` for a horizontal bar — and takes one of:

| Value                      | Order                                                                    |
|----------------------------|--------------------------------------------------------------------------|
| `ascending` / `descending` | By the category **label** (numeric-aware: `"9"` sorts before `"100"`).  |
| `value` / `-value`         | By the category's **measure**, summed across series (`-value` = biggest first). |

`limit: N` on the same channel keeps only the first N categories (after the sort, when both are set)
— together they express a top-N ranking:

```json
"encoding": {
  "x": { "field": "value" },
  "y": [ { "field": "name", "type": "nominal", "sort": "-value", "limit": 5 } ]
}
```

Sorting is stable (ties keep first-seen order) and happens after series alignment, so it composes
with multi-series encodings and a `color` split. Forms without a category axis (bubble, heatmap,
timeline) reject `sort`/`limit`.

`stack: true` stacks a bar's series instead of grouping them side by side: positive values pile up
from zero, negative values pile down, per category. It sits on the bar's **measure channel** (`y[0]`
for a vertical bar, `x` for a horizontal one) and is rejected on other marks and channels. It
combines naturally with `color` (one stacked segment per category value) and with multiple y series:

```json
"encoding": {
  "x": { "field": "time", "type": "nominal" },
  "y": [ { "field": "value", "stack": true } ],
  "color": { "field": "entity", "type": "nominal" }
}
```

The ECharts renderer stacks via per-series stack groups; the SVG renderer computes the stacked
segment coordinates and spans the value axis over the stack totals.

### Filter

A `filter` keeps only records matching **all** of its conditions (logical AND). The shorthand
`{ "field": "entity", "equals": "AAPL" }` is a single equality; for multi-field, range, or period
filtering use `all` with comparison operators:

```json
"filter": {
  "all": [
    { "field": "entity", "value": "AAPL" },
    { "field": "time", "op": "ge", "value": "2026-01-01" },
    { "field": "time", "op": "lt", "value": "2026-04-01" }
  ]
}
```

`op` is one of `eq` (default), `ne`, `lt`, `le`, `gt`, `ge`. Ordered comparisons (`lt`/`le`/`gt`/`ge`)
compare numerically when both the record value and `value` parse as numbers, otherwise lexically — so
ISO timestamps and numeric fields both order correctly. Shorthand and `all` combine.

### Grid charts (heatmap, timeline)

Two drawing forms map records onto a 2D grid of `x` columns × `y[0]` rows (both **nominal**,
accumulated in first-seen order).

- **Heatmap** (`mark: rect`) colors each cell by `encoding.color.field` (light → dark, with a value
  legend); `color` is required. Repeated `(column, row)` cells draw later-record-on-top. A cell with
  no value is gray. Use it for a value-per-pair matrix, e.g. sector × quarter return.

  ```json
  { "version": 2, "mark": "rect", "data": { "source": "returns.jsonl", "kind": "metric" },
    "encoding": { "x": { "field": "time", "type": "nominal", "title": "Quarter" },
                  "y": [ { "field": "entity", "type": "nominal", "title": "Sector" } ],
                  "color": { "field": "value" } } }
  ```

- **Timeline** (`mark: point` with a **nominal y**) places one dot per record at its
  `(x column, y[0] lane)`; an optional `encoding.size.field` scales the dot radius, and each lane gets
  its own color. Use it for a swimlane event strip, e.g. events per entity over time.

  ```json
  { "version": 2, "mark": "point", "data": { "source": "events.jsonl", "kind": "event" },
    "encoding": { "x": { "field": "time", "type": "nominal" },
                  "y": [ { "field": "entity", "type": "nominal" } ] } }
  ```

### Overlays (markers, reference lines, bands)

An overlay draws reference geometry on top of the chart. Each entry in `overlays` is **exactly one**
of three shapes, discriminated by which fields are set (a mixed entry is rejected):

```json
"overlays": [
  { "source": "events.jsonl", "kind": "event", "at": "time", "label": "title" },
  { "y": 100, "axis": "y2", "label": "threshold" },
  { "from": "2026-01-01", "to": "2026-02-01", "label": "earnings window" }
]
```

**Markers** — vertical lines read from a second JSONL source or carried inline, e.g. plotting news
events along a metric time series:

| Field     | Required        | Notes                                                                        |
|-----------|-----------------|------------------------------------------------------------------------------|
| `source`  | one of the two  | Path to a JSONL file, resolved relative to the spec file (like `data.source`).|
| `records` | one of the two  | Marker records carried inline (like `data.records`), keeping an annotated chart self-contained. |
| `kind`    | yes             | A canonical kind (typically `event` or `annotation`).                        |
| `at`      | no              | Field giving the marker's x position; defaults to `time`.                    |
| `label`   | no              | Field giving the marker text; defaults to `text`.                            |

A record with no `at` value is skipped. The marker is placed at the matching x-axis category, so the
`at` value should equal one of the x-axis labels (the renderer uses a category x-axis). Multiple
overlays accumulate.

**Reference line** — a dashed horizontal line at a literal value (a threshold):

| Field   | Required | Notes                                                             |
|---------|----------|-------------------------------------------------------------------|
| `y`     | yes      | The value to draw the line at.                                    |
| `axis`  | no       | `y` (default) or `y2` — which value axis the line is pinned to.  |
| `label` | no       | Literal label text drawn on the line.                             |

**Band** — a shaded x-range highlighting a period:

| Field   | Required | Notes                                                              |
|---------|----------|--------------------------------------------------------------------|
| `from`  | yes      | First x category of the range (inclusive; should match an x label).|
| `to`    | yes      | Last x category of the range (inclusive).                          |
| `label` | no       | Literal label text drawn in the band.                              |

Source marker overlays need file IO, so they resolve in the CLI; line/band overlays (literal values)
and inline marker records travel with the spec and resolve in `Spec.Resolve`, which is why all three
also work for embedded assets (below). A y-range band is deliberately not supported — a value
threshold is a reference line.

### Inline data (self-contained specs)

A spec carries its data either by `data.source` (an external JSONL file) **or** by `data.records` (an
inline array), never both. Inline data makes a spec self-contained — a single file is a complete chart
— which is what the embedded-asset path (below) needs. Inline numbers decode as `float64` (not
`json.Number`); `Record.Float` reads them the same way.

```json
{ "version": 2, "mark": "line", "data": { "kind": "metric",
    "records": [ { "name": "PI", "time": "01", "value": 100 }, { "name": "PI", "time": "02", "value": 110 } ] },
  "encoding": { "x": { "field": "time" }, "y": [ { "field": "value" } ] } }
```

### Embedding a chart as an asset

A self-contained spec saved as a `.viewspec.json` **asset** becomes a chart when a note or doc embeds
it as an image:

```markdown
![Close](assets/chart.viewspec.json)
```

`track export-site` (`internal/track/site`) detects a `.viewspec.json` asset reference, resolves it to
its ECharts option (`render.EChartsOptionFromSpecDir`) at build time, writes the option JSON into the
published `assets/` as `.echarts.json`, and rewrites the reference — the frontend embed fetches it and
draws an interactive chart with its bundled ECharts. Embedded charts must use inline `data.records`
(an asset is resolved in isolation, with no spec-relative file to read); source marker overlays and
`data.source` are not supported on this path, but line/band overlays (literal values) and inline
marker records (`overlays[].records`) render. The live web workspace does not yet render embedded
specs.

### Embedding a chart in a note (fenced `viewspec` block)

A View Spec written directly in a note body renders as a chart, the same way a fenced ` ```mermaid `
block renders as a diagram. Fence the block with the language `viewspec` and write a single View Spec
(JSON, as above) as the body:

````markdown
```viewspec
{ "version": 2, "mark": "line", "title": "PI",
  "data": { "kind": "metric", "source": "metrics.jsonl" },
  "encoding": { "x": { "field": "time" }, "y": [ { "field": "value" } ] } }
```
````

Chart semantics stay decided by the engine on both rendering surfaces; only the drawing runtime
differs:

- **Web workspace**: the frontend posts the block to `POST /api/viewspec` (`{"spec": "..."}`) and
  hands the returned ECharts option (`{"echarts": {...}}`, from `render.EChartsOptionFromSpecDir`) to
  a local ECharts instance (a lazily loaded chunk), so embedded charts are interactive — tooltips,
  legend toggling — without the frontend re-implementing chart resolution. Charts re-render live:
  editing the note re-posts the changed block through the normal note-refresh flow, and the server
  also watches the vault's `data/` directory and emits a `data` Server-Sent Event (alongside the
  existing `change` event for note edits) so displayed charts re-fetch when a `data.source` /
  `overlays[].source` file changes.
- **Static site** (`track export-site`, both the vault and `--dir` front-ends): each fence is
  resolved at build time to a fenced ```` ```echarts ```` block carrying the ready-to-draw option, so
  the published page draws the same interactive chart with the frontend's bundled ECharts (a lazily
  loaded chunk; pages without charts never download it). No chart engine or vault data ships with the
  site — resolution already happened.

Unlike the isolated `.viewspec.json` asset path, a fenced block may use `data.source` (and
`overlays[].source`): paths resolve **inside the vault's `data/` directory** (for `--dir` exports, a
`data/` directory next to the Markdown files). Absolute paths and `..` traversal are rejected, so a
note cannot read files outside the data directory. Inline `data.records` works as well and keeps the
block self-contained.

An invalid spec (or unreadable data) never breaks the page: the web workspace shows the error message
plus the original source at the block position, and the static export publishes an inline error
blockquote followed by the source as a JSON code block.

### Resolution semantics

Applying a spec to records (`Spec.Resolve`) first derives the drawing form from the mark and channel
types, then produces the aligned data that form needs (category-axis series, horizontal bars, linear
bubble points, or a grid), recorded on `Resolved.Chart` so a renderer switches over one concrete shape:

- Records failing the filter are dropped.
- For the series forms, each surviving record contributes its `x` (or, for horizontal bars, its nominal
  `y`) as a label and each measure field as a series value.
- With a `color` channel, records are instead grouped by the color field's value: one series per
  category, aligned to the shared label axis with `NaN` gaps (see "Color" above).
- A record **missing a numeric value** contributes `NaN`, which renders as a gap (not a zero).

## Rendering

Renderers live in `internal/track/render` behind a `Renderer` interface and a name registry, so new
output formats can be added without changing the model or the spec.

### `echarts` (default)

Emits a self-contained HTML page that loads **Apache ECharts from a CDN**
(`https://cdn.jsdelivr.net/npm/echarts@5`) and draws the chart into a full-page container. The page is
interactive — hover tooltips (axis-slice on category charts, per-item on point-shaped ones), legend
toggling, and window-resize tracking come with the library. Chart semantics are decided in Go: the
renderer emits a **pure-JSON ECharts option** (`render.EChartsOptionJSON`), shared by the single-chart
page, article composition, and the web workspace's fenced-block endpoint.

- `line`/`bar` marks map directly to ECharts series over a category x-axis; an `area` is a `line`
  with an `areaStyle` filled at the same 30% palette opacity as the SVG renderer.
- A `bar` with a nominal y renders with the categories down an inverted y-axis (first label on top),
  for rankings; a stacked bar joins its series into one stack group.
- A `candlestick` maps to the native candlestick series (item order `[open, close, low, high]`), with
  the SVG renderer's rising-green/falling-red colors; a candle missing a component is a null gap.
- A `point` with a nominal x (scatter) uses the category axis; a quantitative x (bubble) plots over
  linear axes with a **per-item `symbolSize`** (2× the resolved radius), so the option stays pure JSON
  — no size callbacks; a nominal y (timeline) is a category-lane scatter sized the same way.
- A `rect` (heatmap) uses the native heatmap series with a `visualMap` spanning the same light→dark
  blue ramp as the SVG renderer.
- A series with `axis: "y2"` is bound to a right-hand secondary value axis (its gridlines kept off the
  chart area); single-axis charts define one axis.
- Every chart carries the fixed palette shared with the `svg` renderer, so colors are deterministic
  and identical across renderers. `NaN`/`Inf` values are emitted as JSON `null` (a gap).
- **Overlays** map to built-in mark geometry on the first series: markers as vertical `markLine`
  entries at their category, reference lines as dashed horizontal `markLine` entries (a `y2` line
  rides a y2-bound series and is dropped without one, like the SVG renderer), bands as shaded
  `markArea` ranges.
- The page requires network access at view time to load ECharts.

### `svg`

Emits a **static, self-contained SVG** — no scripts, no CDN, no network access at view time — so the
output embeds directly in notes, emails, or a static site:

- line/area/bar/scatter are drawn over a category x-axis (bars are grouped per series — or stacked
  with `stack: true` — and a nominal-y bar runs categories down the y-axis for rankings; an area
  fills each series down to the zero baseline at 30% opacity, broken at NaN gaps like its line).
- A candlestick draws a high–low wick and an open–close body per category, green for a rising candle
  and red for a falling one; the OHLC component series are not listed in the legend.
- A bubble (quantitative-x point) is drawn over **linear** x and y axes (one circle per `{x, y, r}`
  point, sized by `size`); a point missing x or y is skipped and a missing/non-positive radius falls
  back to a small default, like the ECharts renderer.
- The value axis spans the data range; bars pin the baseline to zero.
- `NaN`/`Inf` values are gaps: a line breaks its segment, a bar/scatter/bubble point is omitted.
- **Overlays** mirror the ECharts mark geometry: markers are vertical lines at the matching category
  label; reference lines are dashed horizontal lines (the SVG renderer has a single value scale, so
  `axis: "y2"` is ignored; a line outside the data's value range is skipped); bands are translucent
  rectangles spanning the `from`..`to` category slots (inclusive), drawn behind the series.

Select it with `track render --renderer svg --out chart.svg`.

## Article (composed document)

An article composes prose, multiple charts, and tables into one HTML page — the "data + layout +
rendering" unit. It is defined in `internal/track/article`: a spec whose top-level has a `blocks`
array.

```json
{
  "version": 1,
  "title": "Market narrative",
  "blocks": [
    { "markdown": "# Overview\n\nNarrative text with **bold** and [links](https://example.com)." },
    { "chart": { "version": 2, "mark": "line", "data": { "source": "metrics.jsonl", "kind": "metric" },
                 "encoding": { "x": { "field": "time" }, "y": [ { "field": "value" } ] } } },
    { "markdown": "Commentary between charts." },
    { "chart": { "version": 2, "mark": "bar", "data": { "source": "ranking.jsonl", "kind": "metric" },
                 "encoding": { "x": { "field": "value" }, "y": [ { "field": "name", "type": "nominal" } ] } } },
    { "table": { "data": { "source": "trades.jsonl", "kind": "event" },
                 "columns": [ { "field": "time", "label": "Date" }, { "field": "entity" } ],
                 "filter": true } }
  ]
}
```

- Each block sets **exactly one** of `markdown` (prose), `chart` (an inline View Spec as above), or
  `table` (a data table over a single source).
- Chart and table data sources (and chart overlays) resolve relative to the article file, like a
  standalone spec.
- A **table** projects its source records onto the named `columns` (one row per record; a missing
  cell renders empty so rows stay aligned). `filter: true` adds a client-side text filter box that
  hides non-matching rows. Tables render as server-side HTML and need no CDN, so they work offline.
- Output is one HTML page: prose is rendered by **marked.js** (CDN) at view time so track keeps no Go
  Markdown dependency; charts reuse the ECharts option builder (ECharts draws every form, including
  candlestick and the grid forms, so nothing needs a fallback). Scripts load conditionally: ECharts
  only if there is a chart, marked only if there is prose, and the table-filter script only if a table
  is filterable.

## CLI

```
track render --spec <spec.json> --out <file> [--renderer echarts]
```

- Loads and validates the spec, resolves its data source relative to the spec file, reads the JSONL,
  renders, and **writes the result to `--out`** (both `--spec` and `--out` are required).
- A spec with a top-level `blocks` array is rendered as an **article** (see above); otherwise as a
  single chart.
- Independent of the note index/store — works on any canonical JSONL, in a vault or not.
- On success prints JSON: `{"path": "...", "renderer": "echarts", "records": N}` for a chart, or
  `{"path": "...", "renderer": "echarts", "blocks": N}` for an article.
- Errors print `{"error": "..."}` with exit code 1, like other track commands.

Examples:

```sh
track render --spec chart.json --out chart.html
track render --spec article.json --out article.html
```

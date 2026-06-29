# Visualization: Canonical Data Model, View Spec, and Rendering

This document specifies how track represents visualization data and how it draws charts from a
declarative spec. Design rationale is in [ADR 0021](../adr/0021-visualization-canonical-data-model.md).

track never fetches data. External sources are converted into the Canonical Data Model below by
separate `track-fetch-*` tools (out of scope here); track only imports, queries, and renders that model.

## Canonical Data Model

The model is defined in `internal/track/dataset`. Data is stored as **JSONL** — one JSON object per
line, one homogeneous file per kind (e.g. `prices.jsonl`). Every record carries a schema `version`
(current: `1`) so the format can evolve.

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
accepted. The render pipeline reads records generically, so a View Spec may address any field present
in the data by name, not only the documented ones.

## View Spec

Defined in `internal/track/viewspec`. A View Spec is a renderer-independent JSON description of one
chart over a single data source. Unknown fields are rejected.

```json
{
  "version": 1,
  "type": "line",
  "title": "AAPL close",
  "data": { "source": "prices.jsonl", "kind": "price" },
  "x": { "field": "time", "label": "Date" },
  "y": [ { "field": "close", "label": "Close" } ],
  "filter": { "field": "entity", "equals": "AAPL" }
}
```

| Field      | Required | Notes                                                                       |
|------------|----------|-----------------------------------------------------------------------------|
| `version`  | yes      | View Spec schema version (current: `1`).                                     |
| `type`     | yes      | `line`, `bar`, `hbar` (ranking), `scatter`, `bubble`, `heatmap`, or `timeline` (last two SVG-only). |
| `title`    | no       | Chart and page title.                                                        |
| `data.source` | yes   | Path to a JSONL file, resolved **relative to the spec file** (or absolute).  |
| `data.kind`   | yes   | One of the canonical kinds.                                                  |
| `x.field`  | yes      | Record field used for x-axis labels (category), or numeric x for `bubble`.   |
| `y`        | yes      | One or more series; each `y[i].field` is a numeric record field.            |
| `size`     | bubble/heatmap | Numeric encoding: bubble radius (required for `bubble`), heatmap cell value (required for `heatmap`), or timeline dot radius (optional). |
| `filter`   | no       | Keep matching records. Shorthand `{field, equals}` or `{all: [{field, op, value}]}` (AND). |
| `overlays` | no       | Vertical event/annotation markers drawn over the chart (see below).         |

`label` overrides the legend/axis text, defaulting to the field name. A y series may set
`"axis": "y2"` to plot on a secondary right-hand axis (default `"y"`), so series on different scales —
e.g. a price and an index — can share one x-axis:

```json
"y": [
  { "field": "close", "label": "Close", "axis": "y" },
  { "field": "index", "label": "Index", "axis": "y2" }
]
```

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

### Grid charts (`heatmap`, `timeline`)

Two **SVG-only** chart types map records onto a 2D grid of `x` columns × `y[0]` rows (both treated as
categories, accumulated in first-seen order). The Chart.js renderer rejects them.

- **`heatmap`** colors each cell by `size.field` (light → dark, with a value legend); `size` is
  required. Repeated `(column, row)` cells draw later-record-on-top. A cell with no value is gray.
  Use it for a value-per-pair matrix, e.g. sector × quarter return.

  ```json
  { "version": 1, "type": "heatmap", "data": { "source": "returns.jsonl", "kind": "metric" },
    "x": { "field": "quarter" }, "y": [ { "field": "sector" } ], "size": { "field": "return" } }
  ```

- **`timeline`** places one dot per record at its `(x column, y[0] lane)`; an optional `size.field`
  scales the dot radius, and each lane gets its own color. Use it for a swimlane event strip, e.g.
  events per entity over time.

  ```json
  { "version": 1, "type": "timeline", "data": { "source": "events.jsonl", "kind": "event" },
    "x": { "field": "date" }, "y": [ { "field": "entity" } ], "size": { "field": "magnitude" } }
  ```

### Overlays (event/annotation markers)

An overlay draws records from a second JSONL source as vertical markers on top of the chart — e.g.
plotting policy events along a Pressure Index time series. Each overlay:

```json
"overlays": [
  { "source": "events.jsonl", "kind": "event", "at": "time", "label": "title" }
]
```

| Field    | Required | Notes                                                                        |
|----------|----------|------------------------------------------------------------------------------|
| `source` | yes      | Path to a JSONL file, resolved relative to the spec file (like `data.source`).|
| `kind`   | yes      | A canonical kind (typically `event` or `annotation`).                        |
| `at`     | no       | Field giving the marker's x position; defaults to `time`.                    |
| `label`  | no       | Field giving the marker text; defaults to `text`.                            |

A record with no `at` value is skipped. The marker is placed at the matching x-axis category, so the
`at` value should equal one of the x-axis labels (the renderer uses a category x-axis). Multiple
overlays accumulate.

### Resolution semantics

Applying a spec to records (`Spec.Resolve`) produces aligned x labels and one series of values per `y`:

- Records failing the filter are dropped.
- Each surviving record contributes its `x` value as a label and each `y` field as a series value.
- A record **missing a y value** contributes `NaN`, which renders as a gap (not a zero).

## Rendering

Renderers live in `internal/track/render` behind a `Renderer` interface and a name registry, so new
output formats can be added without changing the model or the spec.

### `chartjs` (default)

Emits a self-contained HTML page that loads **Chart.js from a CDN**
(`https://cdn.jsdelivr.net/npm/chart.js@4`) and draws the chart in a `<canvas>`:

- `line`/`bar` map directly to Chart.js types over a category x-axis.
- `hbar` renders as a Chart.js `bar` with `indexAxis: "y"` (horizontal), for rankings.
- `scatter` uses the same category x-axis with the connecting line suppressed.
- `bubble` plots one `{x, y, r}` point per record (numeric `x`, `y[i]`, and `size`); points missing a
  coordinate are skipped and a missing/non-positive radius falls back to a small default.
- A series with `axis: "y2"` is bound to a right-hand secondary linear axis (its gridlines kept off the
  chart area); single-axis charts define no `y2`.
- `NaN`/`Inf` values are emitted as JSON `null` (a gap).
- **Overlay markers** are drawn as vertical lines via `chartjs-plugin-annotation`, loaded from a CDN
  only when the spec has overlays (plain charts stay lean).
- The page requires network access at view time to load Chart.js (and the annotation plugin, if used).

### `svg`

Emits a **static, self-contained SVG** — no scripts, no CDN, no network access at view time — so the
output embeds directly in notes, emails, or a static site:

- `line`/`bar`/`hbar`/`scatter` are drawn over a category x-axis (bars are grouped per series; `hbar`
  runs categories down the y-axis for rankings).
- The value axis spans the data range; `bar`/`hbar` pin the baseline to zero.
- `NaN`/`Inf` values are gaps: a `line` breaks its segment, a `bar`/`scatter` point is omitted.
- **Overlay markers** are vertical lines at the matching category label (mirroring the Chart.js
  annotation overlays).
- `bubble` is **not supported yet** (its `{x, y, r}` shape needs linear axes); use `--renderer chartjs`.

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
    { "chart": { "version": 1, "type": "line", "data": { "source": "metrics.jsonl", "kind": "metric" },
                 "x": { "field": "time" }, "y": [ { "field": "value" } ] } },
    { "markdown": "Commentary between charts." },
    { "chart": { "version": 1, "type": "hbar", "data": { "source": "ranking.jsonl", "kind": "metric" },
                 "x": { "field": "name" }, "y": [ { "field": "value" } ] } },
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
  Markdown dependency; charts reuse the Chart.js renderer. The annotation plugin loads only if a chart
  has overlays, marked only if there is prose, and the table-filter script only if a table is
  filterable.

## CLI

```
track render --spec <spec.json> --out <file> [--renderer chartjs]
```

- Loads and validates the spec, resolves its data source relative to the spec file, reads the JSONL,
  renders, and **writes the result to `--out`** (both `--spec` and `--out` are required).
- A spec with a top-level `blocks` array is rendered as an **article** (see above); otherwise as a
  single chart.
- Independent of the note index/store — works on any canonical JSONL, in a vault or not.
- On success prints JSON: `{"path": "...", "renderer": "chartjs", "records": N}` for a chart, or
  `{"path": "...", "renderer": "chartjs", "blocks": N}` for an article.
- Errors print `{"error": "..."}` with exit code 1, like other track commands.

Examples:

```sh
track render --spec chart.json --out chart.html
track render --spec article.json --out article.html
```

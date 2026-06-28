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
| `type`     | yes      | `line`, `bar`, or `scatter`.                                                 |
| `title`    | no       | Chart and page title.                                                        |
| `data.source` | yes   | Path to a JSONL file, resolved **relative to the spec file** (or absolute).  |
| `data.kind`   | yes   | One of the canonical kinds.                                                  |
| `x.field`  | yes      | Record field used for x-axis category labels.                               |
| `y`        | yes      | One or more series; each `y[i].field` is a numeric record field.            |
| `filter`   | no       | Keep only records where `filter.field` equals `filter.equals` (string).     |
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
- `scatter` uses the same category x-axis with the connecting line suppressed.
- A series with `axis: "y2"` is bound to a right-hand secondary linear axis (its gridlines kept off the
  chart area); single-axis charts define no `y2`.
- `NaN`/`Inf` values are emitted as JSON `null` (a gap).
- **Overlay markers** are drawn as vertical lines via `chartjs-plugin-annotation`, loaded from a CDN
  only when the spec has overlays (plain charts stay lean).
- The page requires network access at view time to load Chart.js (and the annotation plugin, if used).

## CLI

```
track render --spec <spec.json> --out <file> [--renderer chartjs]
```

- Loads and validates the spec, resolves its data source relative to the spec file, reads the JSONL,
  renders, and **writes the result to `--out`** (both `--spec` and `--out` are required).
- Independent of the note index/store — works on any canonical JSONL, in a vault or not.
- On success prints JSON: `{"path": "...", "renderer": "chartjs", "records": N}`.
- Errors print `{"error": "..."}` with exit code 1, like other track commands.

Example:

```sh
track render --spec chart.json --out chart.html
```

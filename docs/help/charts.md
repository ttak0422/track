# Charts

`track render` turns a declarative **View Spec** (JSON) into a chart, reading data from the
**Canonical Data Model** — plain JSONL files, one record per line, one kind per file. track never
fetches data itself; external sources are converted into this model by separate `track-fetch-*` tools.

Part of [[Visualization]] (see also [[Diagrams]] and [[Embeds]]). Back to [[track]].

## Data: the Canonical Data Model

A data file is JSONL with one homogeneous *kind* per file. Every record carries a schema `version`.

| Kind | Required fields | Meaning |
| --- | --- | --- |
| `event` | `time`, `title` | A point-in-time happening (news, post, milestone). |
| `price` | `entity`, `time`, `open`, `high`, `low`, `close` | One OHLCV bar. |
| `metric` | `name`, `time`, `value` | A named numeric series sample (e.g. a custom index). |
| `entity` | `id`, `name` | A thing series refer to (ticker, index, sector). |
| `annotation` | `time`, `text` | A label for narrative overlays. |

The full field list (with optional fields and types) is printed by `track render --help`, derived from
the typed structs so it never drifts. Example `series.jsonl` (a `metric` kind):

```jsonl
{"version":1,"name":"demo","time":"1","value":3}
{"version":1,"name":"demo","time":"2","value":7}
{"version":1,"name":"demo","time":"3","value":4}
```

`kind` is a real schema: rendering validates each record against it and **fails with an error** if a
required field is missing or a numeric field is non-numeric, rather than drawing a partial chart. Extra
fields beyond the schema are allowed, so a spec can still chart custom columns.

Canonical data files live in the vault's `data/` directory (created by `track init`) — that is where
`track-fetch-*` tools write their output. A spec references a file by path, so data may live anywhere a
spec can reach.

## View Spec: one chart

A View Spec names a data source, a **mark** (what to draw), and an **encoding** that maps record fields
onto visual channels. It knows nothing about the renderer, so the same spec can be drawn by different
backends.

```json
{
  "version": 2,
  "mark": "line",
  "title": "Line",
  "data": {
    "kind": "metric",
    "records": [
      { "name": "demo", "time": "1", "value": 3 },
      { "name": "demo", "time": "2", "value": 7 },
      { "name": "demo", "time": "3", "value": 4 }
    ]
  },
  "encoding": {
    "x": { "field": "time" },
    "y": [ { "field": "value" } ]
  }
}
```

```sh
track render --spec chart.json --out chart.html
```

Rendered output:

![Line chart](assets/chart-line.viewspec.json)

Key fields:

- `mark` — `line`, `bar`, `point`, `area`, `rect`, `candlestick`, or `treemap`.
- `encoding.*.type` — `quantitative` (default) or `nominal` (a category). Nominal on the right axis
  picks the form: a nominal-y `bar` is horizontal, a nominal-x `point` is a scatter (vs a bubble), and
  `rect` needs nominal x and y.
- `encoding.y[].axis` — set `"y2"` to put a series on a secondary right-hand axis (two series on different scales).
- `encoding.color` — a nominal field that splits records into one colored series per value
  (on `rect` it is instead the quantitative heatmap cell value).
- `sort` / `limit` — on the category-axis channel: order categories by label or value
  (`ascending|descending|value|-value`) and keep only the first N (top-N).
- `stack` — on a bar's measure channel (`y[0]`, or `x` for a horizontal bar): stack the series.
- `filter` — `{field, equals}` shorthand, or `{all: [{field, op, value}]}` with `op` of
  `eq|ne|lt|le|gt|ge` for multi-field, range, and period filtering.
- `y[].mark` — `line|bar|area` on a y channel draws that series in its own form: a **combo chart**
  (volume bars + an index line on `y2`). Dense category charts also zoom automatically: wheel/pinch
  always, plus a range slider past 30 categories.
- `y[].window` — replace the series with its rolling mean over the trailing N records (a moving
  average); the first N-1 points stay empty.
- `overlays` — reference geometry over the chart: `{source, kind}` (or `{records, kind}` with the
  events inline) draws events/annotations as vertical markers, `{y, axis?, label?}` a horizontal
  threshold line, `{from, to, label?}` a shaded period band, and `{x, y, label}` a callout bubble
  pointing at one data point. A marker overlay with `display: "box"` also draws each event as an
  always-visible annotation box hugging the plot (web reader only).

## Embedding a chart in a note

Every image on this page is a **View Spec asset rendered by track**, not a hand-made picture. A
`.viewspec.json` asset carries the spec *and its data inline* (`data.records`), so it is a complete,
self-contained chart. Embed it like any image and track resolves it at build time; the published page
draws it as an interactive ECharts chart:

```markdown
![Line](assets/chart-line.viewspec.json)
```

Just drop the `.viewspec.json` file into the assets directory and reference it — no `import` step is
required (`track asset import` only copies a file there for you). There is no separate data file to
keep in sync and no CDN — the engine resolves the spec at build time and the site's bundled ECharts
draws it. The charts below are each one embedded `.viewspec.json`.

### Writing the spec inline

Prefer to keep the spec in the note itself? Fence a block with `viewspec` — the same way a `mermaid`
fence embeds a [[Diagrams|diagram]] — and it renders as an interactive ECharts chart (hover
tooltips, legend toggling) in both the web workspace and the published site.
Inline `data.records` keeps the block self-contained; `data.source` reads a JSONL file
from the vault's `data/` directory. If the spec is invalid, the error and the source are shown at the
block's position, so a typo never hides your text:

````markdown
```viewspec
{
  "version": 2,
  "mark": "bar",
  "title": "Notes per week",
  "data": {
    "kind": "metric",
    "records": [
      { "name": "notes", "time": "W1", "value": 4 },
      { "name": "notes", "time": "W2", "value": 7 },
      { "name": "notes", "time": "W3", "value": 5 },
      { "name": "notes", "time": "W4", "value": 9 }
    ]
  },
  "encoding": {
    "x": { "field": "time", "type": "nominal", "title": "Week" },
    "y": [ { "field": "value", "title": "Notes" } ]
  }
}
```
````

It renders as (live):

```viewspec
{
  "version": 2,
  "mark": "bar",
  "title": "Notes per week",
  "data": {
    "kind": "metric",
    "records": [
      { "name": "notes", "time": "W1", "value": 4 },
      { "name": "notes", "time": "W2", "value": 7 },
      { "name": "notes", "time": "W3", "value": 5 },
      { "name": "notes", "time": "W4", "value": 9 }
    ]
  },
  "encoding": {
    "x": { "field": "time", "type": "nominal", "title": "Week" },
    "y": [ { "field": "value", "title": "Notes" } ]
  }
}
```

**`area`** — a line with the region down to zero filled; every line channel (color split, y2, sort) works unchanged.

![Area chart](assets/chart-area.viewspec.json)

**`bar`** — values per category; the baseline is pinned to zero, so negatives drop below it.

![Bar chart](assets/chart-bar.viewspec.json)

**Horizontal bar** (`mark: bar`, nominal y) — for rankings; categories run down the left, the value axis along the bottom.

![Horizontal bar chart](assets/chart-hbar.viewspec.json)

**Scatter** (`mark: point`, nominal x) — points over a category x-axis, the connecting line suppressed.

![Scatter chart](assets/chart-scatter.viewspec.json)

**Color split** (`encoding.color`, nominal) — one series per category value, each in its own color; works on `line`, `bar`, and `point`.

![Color split line chart](assets/chart-color.viewspec.json)

**Sort and top-N** (`sort`, `limit` on the category-axis channel) — order categories by label
(`ascending`/`descending`) or by their value total (`value`/`-value`), and keep only the first N.
Here a ranking: the nominal y sorted `-value` with `limit: 5`.

![Top-5 ranking](assets/chart-sort.viewspec.json)

**Stacked bars** (`stack: true` on the bar's measure channel) — series pile up per category instead
of sitting side by side; combines with a color split. On a horizontal bar, set it on `encoding.x`.

![Stacked bar chart](assets/chart-stack.viewspec.json)

**Combo** (`y[].mark`) — series in mixed forms: volume bars with an index line on the secondary axis.

![Combo chart](assets/chart-combo.viewspec.json)

**Heatmap** (`mark: rect`) — a 2D grid of `x` columns × `y[0]` rows, each cell colored by `encoding.color` (with a value legend).

![Heatmap](assets/chart-heatmap.viewspec.json)

**Timeline** (`mark: point`, nominal y) — one dot per record at its `(column, lane)`; an optional `size` scales the dot, one color per lane.

![Timeline](assets/chart-timeline.viewspec.json)

**Candlestick** (`mark: candlestick`, `kind: price`) — one OHLC candle per record: a high–low wick
behind an open–close body, green when the close is at or above the open, red otherwise. The
open/high/low/close fields are implied by the `price` kind, so the encoding needs only `x`.

![Candlestick chart](assets/chart-candlestick.viewspec.json)

**Moving averages and volume** (`encoding.y` on a candlestick) — explicit y channels draw extra
series over the candles: `window: N` turns a field into its rolling mean (`close` + `window: 25` =
MA25, starting as a gap until N records exist), and `{"field": "volume", "mark": "bar", "axis": "y2"}`
adds volume bars in a bottom band, each colored by its candle's direction (green rising, red falling).

```json
"y": [
  { "field": "close", "window": 5,  "mark": "line", "title": "MA5" },
  { "field": "close", "window": 25, "mark": "line", "title": "MA25" },
  { "field": "volume", "mark": "bar", "axis": "y2", "title": "Volume" }
]
```

![Candlestick with MA and volume](assets/chart-candle-ma.viewspec.json)

**Treemap** (`mark: treemap`) — the industry-map view: one rectangle per record, its area from
`encoding.size` and its color from `encoding.color`, grouped one level by an optional nominal `y[0]`;
the nominal `x` names each leaf. With `"scale": "diverging"` on the color channel the ramp centers on
zero — negatives red, positives green, like the candlestick colors — so a market-cap map of daily
change reads at a glance. It is axis-less: `sort`/`limit` and overlays are rejected.

![Treemap](assets/chart-treemap.viewspec.json)

**Overlays** — a vertical `{records, kind}` event marker, a dashed `{y, label}` threshold line, a
shaded `{from, to, label}` period band, and a `{x, y, label}` callout bubble pointing at a data point,
over any category-axis chart. All four travel with the spec (inline records or literal values — no
second data file), so they work in embedded assets too.

![Event marker, threshold line, and period band](assets/chart-overlay.viewspec.json)

**Annotation boxes** (`display: "box"` on a marker overlay) — each event also becomes a small
always-visible box hugging the plot, boxes alternating above and below it: the date (the `at` value,
trimmed to its day), the wrapped label, and a source link taken from the record's `url`. The chart
doubles as a scannable index of its evidence. The rail is drawn by the web reader (workspace and
published site); the standalone HTML page, the composed article, and the SVG renderer keep the
classic marker look.

```json
"overlays": [
  {
    "records": [
      { "time": "2026-01-12", "title": "v1.0 released", "url": "https://example.com/release" }
    ],
    "kind": "event",
    "label": "title",
    "display": "box"
  }
]
```

![Annotation boxes](assets/chart-box.viewspec.json)

A **bubble** (`mark: point` with a quantitative x, `{x, y, r}` points sized by `size`) is drawn over
linear axes by both the default `echarts` renderer and the `svg` renderer.

## Renderers

| Renderer | Output | Notes |
| --- | --- | --- |
| `echarts` (default) | Self-contained HTML | Interactive (tooltips, legend toggling); loads Apache ECharts from a CDN at view time. All marks. |
| `svg` | Static SVG | No scripts, no CDN — embeds anywhere. All marks, including heatmap, timeline, and candlestick. |

```sh
track render --spec chart.json --out chart.svg --renderer svg
```

## Articles: prose + charts in one page

A spec with a `blocks` array composes Markdown prose, multiple charts, and filterable tables into a
single HTML article — the "data + layout + rendering" unit for a data story.

The full notation (every field, all marks, examples) is always available with:

```sh
track render --help
```

tags:: help/visualization/charts
section:: visualization
cover:: assets/cover-charts.svg
reviewed:: 2026-06-24

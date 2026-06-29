# Visualization

`track render` turns a declarative **View Spec** (JSON) into a chart, reading data from the
**Canonical Data Model** — plain JSONL files, one record per line, one kind per file. track never
fetches data itself; external sources are converted into this model by separate `track-fetch-*` tools.

Back to [[track]].

## Data: the Canonical Data Model

A data file is JSONL with one homogeneous *kind* per file. Every record carries a schema `version`.

| Kind | Required fields | Meaning |
| --- | --- | --- |
| `event` | `time`, `title` | A point-in-time happening (news, post, milestone). |
| `price` | `entity`, `time`, `open`, `high`, `low`, `close` | One OHLCV bar. |
| `metric` | `name`, `time`, `value` | A named numeric series sample (e.g. a Pressure Index). |
| `entity` | `id`, `name` | A thing series refer to (ticker, index, sector). |
| `annotation` | `time`, `text` | A label for narrative overlays. |

The full field list (with optional fields and types) is printed by `track render --help`, derived from
the typed structs so it never drifts. Example `prices.jsonl`:

```jsonl
{"version":1,"entity":"AAPL","time":"2026-06-01","open":190,"high":195,"low":189,"close":194}
{"version":1,"entity":"AAPL","time":"2026-06-02","open":194,"high":198,"low":193,"close":197}
```

## View Spec: one chart

A View Spec names a data source, the chart type, and how record fields map onto axes. It knows nothing
about the renderer, so the same spec can be drawn by different backends.

```json
{
  "version": 1,
  "type": "line",
  "title": "AAPL close",
  "data": { "source": "prices.jsonl", "kind": "price" },
  "x": { "field": "time" },
  "y": [ { "field": "close", "label": "Close" } ],
  "filter": { "field": "entity", "equals": "AAPL" }
}
```

```sh
track render --spec chart.json --out chart.html
```

Rendered output (a line series with an event drawn as an overlay marker):

![Line chart with an event overlay](assets/chart-line.svg)

Key fields:

- `type` — `line`, `bar`, `hbar` (ranking), `scatter`, `bubble`, or the SVG-only `heatmap` / `timeline`.
- `y[].axis` — set `"y2"` to put a series on a secondary right-hand axis (e.g. price + index).
- `filter` — `{field, equals}` shorthand, or `{all: [{field, op, value}]}` with `op` of
  `eq|ne|lt|le|gt|ge` for multi-field, range, and period filtering.
- `overlays` — draw events/annotations from a second source as vertical markers over a time series.

## Chart types

Every image below is a real `track render --renderer svg` output (static, self-contained SVG).

**`bar`** — values per category; the baseline is pinned to zero, so negatives drop below it.

![Bar chart of sector returns](assets/chart-bar.svg)

**`hbar`** — a horizontal bar, for rankings; categories run down the left, the value axis along the bottom.

![Horizontal bar ranking by exposure](assets/chart-hbar.svg)

**`scatter`** — points over a category x-axis, the connecting line suppressed.

![Scatter of sector returns](assets/chart-scatter.svg)

**`heatmap`** — a 2D grid of `x` columns × `y[0]` rows, each cell colored by `size` (with a value legend).

![Heatmap of value by sector and quarter](assets/chart-heatmap.svg)

**`timeline`** — one dot per record at its `(column, lane)`; an optional `size` scales the dot, one color per lane.

![Timeline of events per quarter by sector](assets/chart-timeline.svg)

`bubble` (`{x, y, r}` points sized by `size`) is drawn by the default `chartjs` renderer; the `svg`
renderer covers the types shown above.

## Renderers

| Renderer | Output | Notes |
| --- | --- | --- |
| `chartjs` (default) | Self-contained HTML | Interactive; loads Chart.js from a CDN at view time. |
| `svg` | Static SVG | No scripts, no CDN — embeds anywhere. line/bar/hbar/scatter, heatmap, timeline. |

```sh
track render --spec chart.json --out chart.svg --renderer svg
```

## Articles: prose + charts in one page

A spec with a `blocks` array composes Markdown prose, multiple charts, and filterable tables into a
single HTML article — the "data + layout + rendering" unit for a data story.

The full notation (every field, all chart types, examples) is always available with:

```sh
track render --help
```

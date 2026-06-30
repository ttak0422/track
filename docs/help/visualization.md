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

A View Spec names a data source, the chart type, and how record fields map onto axes. It knows nothing
about the renderer, so the same spec can be drawn by different backends.

```json
{
  "version": 1,
  "type": "line",
  "title": "Line",
  "data": {
    "kind": "metric",
    "records": [
      { "name": "demo", "time": "1", "value": 3 },
      { "name": "demo", "time": "2", "value": 7 },
      { "name": "demo", "time": "3", "value": 4 }
    ]
  },
  "x": { "field": "time" },
  "y": [ { "field": "value" } ]
}
```

```sh
track render --spec chart.json --out chart.html
```

Rendered output:

![Line chart](assets/chart-line.viewspec.json)

Key fields:

- `type` — `line`, `bar`, `hbar` (ranking), `scatter`, `bubble`, or the SVG-only `heatmap` / `timeline`.
- `y[].axis` — set `"y2"` to put a series on a secondary right-hand axis (two series on different scales).
- `filter` — `{field, equals}` shorthand, or `{all: [{field, op, value}]}` with `op` of
  `eq|ne|lt|le|gt|ge` for multi-field, range, and period filtering.
- `overlays` — draw events/annotations from a second source as vertical markers over a time series.

## Embedding a chart in a note

Every image on this page is a **View Spec asset rendered by track**, not a hand-made picture. A
`.viewspec.json` asset carries the spec *and its data inline* (`data.records`), so it is a complete,
self-contained chart. Embed it like any image and track renders it to a static SVG when the site is
built:

```markdown
![Line](assets/chart-line.viewspec.json)
```

Just drop the `.viewspec.json` file into the assets directory and reference it — no `import` step is
required (`track asset import` only copies a file there for you). There is no separate data file to keep
in sync, no CDN, and no client-side JavaScript — the engine turns the spec into an SVG image. The charts
below are each one embedded `.viewspec.json`.

**`bar`** — values per category; the baseline is pinned to zero, so negatives drop below it.

![Bar chart](assets/chart-bar.viewspec.json)

**`hbar`** — a horizontal bar, for rankings; categories run down the left, the value axis along the bottom.

![Horizontal bar chart](assets/chart-hbar.viewspec.json)

**`scatter`** — points over a category x-axis, the connecting line suppressed.

![Scatter chart](assets/chart-scatter.viewspec.json)

**`heatmap`** — a 2D grid of `x` columns × `y[0]` rows, each cell colored by `size` (with a value legend).

![Heatmap](assets/chart-heatmap.viewspec.json)

**`timeline`** — one dot per record at its `(column, lane)`; an optional `size` scales the dot, one color per lane.

![Timeline](assets/chart-timeline.viewspec.json)

`bubble` (`{x, y, r}` points sized by `size`) is drawn over linear axes by both the default `chartjs`
renderer and the `svg` renderer.

## Renderers

| Renderer | Output | Notes |
| --- | --- | --- |
| `chartjs` (default) | Self-contained HTML | Interactive; loads Chart.js from a CDN at view time. |
| `svg` | Static SVG | No scripts, no CDN — embeds anywhere. line/bar/hbar/scatter/bubble, heatmap, timeline. |

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

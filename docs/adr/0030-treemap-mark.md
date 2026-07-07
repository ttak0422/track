# 0030. Treemap mark

Status: Accepted

## Context

The finviz-style industry heatmap — rectangles sized by market cap, colored by change %, grouped by
sector, one ticker per leaf — is a staple of the market articles track's charts retell. Nothing in
the View Spec draws it: every existing form hangs off a category or value axis, and the color
channel's quantitative reading exists only on `rect`. The mark + encoding model (ADR 0024) prices a
new form at "one mark", provided it reuses the existing channels.

## Decision

Add a `treemap` mark, the spec's first **axis-less** form, with **no new encoding channels**:
`x` (nominal, required) is the leaf label, `y[0]` (nominal, optional, at most one) the single group
level, `size` (required) the rectangle area, and `color` (required) the quantitative cell value —
the same reinterpretation of `color` that `rect` already makes.

- **One new channel option, shared with rect.** `encoding.color` gains `scale: "diverging"`: a
  zero-centered ramp running the candlestick red → neutral → green (negative → positive, the market
  convention) over a domain symmetric around zero. The default stays the sequential heatmap ramp.
  Placement is validated like `sort`/`stack`: only on `color`, only where color is a quantitative
  cell value (`rect`, `treemap`); an error anywhere else.
- **Resolution.** The spec resolves to a `Tree` (one node per record: label, group, size, value)
  next to `Grid`, in record order so group accumulation stays first-seen deterministic. Resolve
  skips nothing; both renderers skip a node without a positive finite size or a finite value (no
  area cannot be drawn, no value has no color).
- **Axis-less means no axis vocabulary.** `sort`/`limit`, the provenance channels, and **all**
  overlays are rejected on treemap — reference geometry anchors on axes it does not have — keeping
  the strict-schema stance rather than silently dropping options.
- **Both renderers** (ADR 0026's implemented-twice norm). ECharts: the native treemap series with
  `[size, value]` leaf items and a continuous `visualMap` on dimension 1 (verified to drive treemap
  cell colors), pure JSON, breadcrumb and click-zoom off. SVG: a deterministic squarified layout in
  Go — groups partitioned by summed size with heading bands, leaves labelled only when the text
  fits — with the ramp interpolated from the same color stops.

## Consequences

- An industry map is one small spec over `metric` records; the demo asset on the charts help page
  renders on every surface (CLI HTML, SVG, web reader, published site).
- Diverging heatmaps come for free, since rect shares the scale option and both renderers route
  through one ramp helper per side.
- The web theme swaps only the 2-stop sequential `visualMap` ramp; a 3-stop diverging ramp keeps
  its semantic market colors in both themes, like candlestick up/down.
- Deferred: a second grouping level, leaf value labels inside cells, and treemap-specific
  interactions (drill-down) — each would be new vocabulary and waits for a need.

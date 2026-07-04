# 26. Apache ECharts as the interactive renderer

Date: 2026-07-05

## Status

Accepted. Amends ADR 0021 (which shipped a Chart.js MVP renderer) and ADR 0022 (whose article
composition reused it).

## Context

The renderer registry (ADR 0021) held three renderers: `chartjs` (interactive HTML via CDN), `svg`
(static, dependency-free), and the article composer reusing the Chart.js config builder. Chart.js
kept hitting expressiveness walls:

- No candlestick type, and no 2D grid forms — heatmap, timeline, and candlestick were SVG-only, so
  articles needed a static-SVG fallback path and interactive output simply lacked those marks.
- Overlay geometry (event markers, reference lines, bands) needed a separate annotation plugin and a
  second conditional CDN.
- Static SVG covers embedding, but as the visualization surface grows (tooltips, richer annotation,
  zoom), a static image is a ceiling, and every interactive gap would mean more per-renderer code.

Maintaining three renderers also multiplies the cost of every new channel or mark — the exact
`N × M` growth the mark+encoding pivot (ADR 0024) was adopted to stop.

## Decision

Replace Chart.js with **Apache ECharts** as the single interactive renderer (`echarts`, the default),
keeping `svg` as the single static one. No compatibility shim (project policy prefers the better
design over backward compatibility).

- ECharts covers **every drawing form natively**: candlestick, heatmap (`visualMap`), category-lane
  scatter (timeline), per-point symbol sizes (bubble), and overlay geometry via built-in
  `markLine`/`markArea` — no plugin, no per-mark fallback. The article composer drops its inline-SVG
  special case.
- The renderer emits a **pure-JSON option** (`render.EChartsOptionJSON`): per-item sizes instead of
  size callbacks, so the same option serializes to the standalone HTML page, the composed article,
  and API responses. Chart semantics stay decided in Go; JavaScript surfaces only instantiate.
- Shared visual constants (series palette, area fill opacity, candle up/down colors, the heatmap
  ramp) keep `echarts` and `svg` output visually consistent, as before.
- The CDN reference (`echarts@5` on jsDelivr) follows the same reasoning as ADR 0021's Chart.js CDN:
  simple, dependency-free output; bundling remains a renderer-local change if offline HTML is needed.

## Consequences

- Interactive output gains tooltips, legend toggling, and all marks; `--renderer svg` remains the
  no-CDN static path (embeds, static site, e-mail).
- Two renderers instead of three: each new View Spec capability is implemented twice, not three
  times, and the interactive/static split is exactly one axis (interactivity).
- Generated HTML still requires network access at view time; fully offline interactive pages would
  need bundling (unchanged trade-off).
- The web workspace can hand the option JSON to its own ECharts instance for interactive embedded
  charts, replacing server-rendered static SVG in the reader (the static site export keeps
  build-time SVG images).

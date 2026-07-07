# 0029. JavaScript delivery: bundle the app surfaces, pin the CDN artifacts

Status: Accepted

## Context

track emits JavaScript-running pages on three kinds of surface:

- the **live web workspace** (`track web`), a Vite-built React frontend embedded in the Go binary;
- the **static site** (`track export-site`), the same frontend in static mode copied into the export;
- **standalone render artifacts** (`track render`): a single-chart HTML page and the composed
  article, generated as one self-contained file.

The heavy libraries (ECharts, Mermaid, KaTeX, pdf.js) can each come from the bundle or a CDN, and the
choice had drifted per surface: the frontend bundles ECharts 6 as a lazy chunk while the standalone
page loaded a floating `echarts@5` from jsDelivr — two different major versions drawing "the same"
pure-JSON option (ADR 0026), with no test noticing. A policy is needed for which surface loads what
from where, and how versions stay in step.

## Decision

- **App surfaces bundle everything.** The web workspace and the static export load all JavaScript
  from the Vite build: heavy libraries are lazily loaded, content-hashed chunks (pages that never
  show a chart/diagram/PDF never fetch those chunks). No CDN scripts. The workspace works offline
  and the exported site has no third-party runtime dependency. The one exception is a third-party
  *service* script that cannot be self-hosted — Twitter's `widgets.js` for tweet embeds (and service
  iframes like YouTube); notes without such embeds load nothing remote.
- **Standalone artifacts stay CDN-backed but exact-pinned.** A single `chart.html` or composed
  article keeps loading ECharts (and marked for article prose) from jsDelivr — bundling would put
  ~1 MB of library into every generated file, defeating the "small self-contained artifact" point
  (ADR 0019, ADR 0026). The ECharts reference is pinned to the exact version `web/package.json`
  bundles, so one chart option renders identically on every surface.
- **A test guards the pin.** `TestEChartsCDNMatchesWebBundle` (render package) reads
  `web/package.json` and fails when the CDN constant and the bundled dependency drift; bumping the
  frontend's ECharts forces the same bump in the artifact renderer.

marked has no bundled counterpart (the frontend renders Markdown with remark), so it stays on a
major-version pin (`marked@12`) with nothing to unify against.

## Consequences

- The workspace and exported sites keep working with no network beyond their own origin; artifact
  pages still need network access at view time (already documented in the visualization spec).
- ECharts upgrades are atomic across surfaces: `web/package.json` and `echartsCDN` move together or
  the render tests fail.
- Standalone artifacts no longer float to whatever `echarts@5` resolves to; behavior changes only
  when track deliberately bumps the pin (verified against real jsDelivr + Chromium at the 6.1.0
  switch: line, combo, candlestick, heatmap, and article pages all draw cleanly).

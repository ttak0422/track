# 0077. Generic metrics dashboards are one reader block

Status: Accepted

## Context

ADR 0076 imported Grafana dashboard JSON by flattening its panels into separate viewspec fences.
That reused the chart engine, but discarded layout, reduced `stat` to a line chart, and gave the
panels no shared selection. A list of charts could not serve as a monitoring dashboard.

The dashboard is a generic track feature. External writers, including track-finance, own data
acquisition and domain computations. A CPU sample and an RSI sample use the same metric model.

## Decision

Preserve Grafana classic JSON's documented subset in one `metrics-dashboard` fence. A shared Go
resolver supplies the live Web and Native readers with panel positions, latest values, formatted
units and thresholds, sample timestamps, and existing ECharts options. The readers own interaction
and layout, not metric evaluation. The existing `dashboard` fence remains the recent-note/journal
widget; existing `viewspec` fences remain standalone charts.

Support `stat`, `timeseries`, and latest-value `table` panels; 24-column `gridPos`; common inclusive
UTC date or instant bounds, entity and metric-name selection; explicit refresh; and invalidation
when vault data changes. Latest values are taken per series after filtering. Empty selections are
empty, not zero. A file error stays in its panel so other panels remain usable. Invalid config or
selection is a dashboard error. Numeric threshold bands do not create alerts or notifications.

Keep datasource files confined to the selected vault's data directory, including symlink targets.
Read each referenced file once per request. There is no data fetch, scheduler, time-series database,
or financial interpretation in the resolver.

`metrics dashboard` validates data and emits the new fence. This intentionally changes the generated
Markdown from ADR 0076; already-created viewspec notes still work. See the specification for limits.

## Consequences

The first version uses JSON-authored placement rather than a drag editor. It does not implement
PromQL, Grafana variables, transformations, arbitrary reductions, or static-site interactivity.
Static exports resolve dashboards with the same Go resolver and emit `metrics-dashboard-snapshot`
blocks containing panel values and ECharts options. The Web reader reuses its panel renderer without
live queries or shared filter controls. Chart tooltips and legends remain available. Source JSONL
files are not copied; the visible series values are included in the published output. Updating them
requires rebuilding the site. This also lets the help site show actual dashboard examples.

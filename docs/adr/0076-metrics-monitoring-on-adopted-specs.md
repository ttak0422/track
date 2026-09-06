# 0076. Metrics monitoring on adopted specs

Status: Accepted

## Context

track owns metric computation and visualization (ADR 0021). What is missing is the monitoring loop
around it: ingesting outside metrics, drawing dashboards, and evaluating threshold rules — for
system metrics (service levels, node exporters, app instrumentation) first of all; market series
from finance writers are one source among others, not the shape of the feature. In this area a
bespoke design would isolate track from the ecosystem it wants to join, so the rule here is
adopt-don't-invent: take the wire and document formats the monitoring world already shares, and map
them onto the Canonical Data Model instead of extending it. Domain computations (RSI, moving
averages, screeners) stay outside the engine, in the skills and fetchers that own their domains.

## Decision

Three adopted surfaces, each a documented subset. Everything outside the subset is a loud error, not
a silent guess.

1. **Ingest OpenMetrics / Prometheus exposition text** (`track metrics scrape`). Parsing is the
   ecosystem's own `prometheus/common/expfmt` — track writes no exposition parser. Samples become
   `metric` records: gauge/counter values as-is (counter keeps its `_total` sample name);
   histogram/summary families flatten to their `_bucket`/`_sum`/`_count` sample names. Labels fold
   into the two fields the model has: `entity` takes the first present of `--entity-labels`
   (default `entity,symbol,instance`); every other label appends to the name in Prometheus display
   form (`name{k="v"}`). Unstamped samples take the scrape time (`--asof` overrides it). track
   never fetches: pipe an endpoint's body in (`curl -s host:9100/metrics | track metrics scrape
   --from -`), per ADR 0021.
2. **Dashboards are Grafana classic JSON, subset** (`track metrics dashboard`). Accepted: `title`,
   `panels[]` of type `timeseries` or `stat`, each target's `expr` as a metric name plus `=` label
   matchers only (no functions — derived values are precomputed upstream, by whatever writer owns
   the domain), and `fieldConfig.defaults` `unit`/`min`/`max`/`thresholds`. The datasource
   convention is `{"type":"track","uid":"<bare data/ filename>"}`. Panels resolve to viewspec
   blocks (timeseries → line with threshold overlays; stat → the same series as a line, documented
   limitation), emitted as note Markdown for embedding — rendering stays on the existing viewspec
   path, so no new renderer is added.
3. **Alerts are Prometheus rule YAML, subset** (`track metrics alert`). Accepted per rule: `alert`,
   `expr` as `metric{matchers} OP number` (OP in `> < >= <= == !=`), `for` as a count of consecutive
   trailing points, `labels`, `annotations`. Evaluation reads the latest values under `--data-dir`
   and prints firing alerts as JSON; `--capture "Note#Heading"` appends dated bullets through the
   existing capture path, so firing becomes vault content instead of stdout.

## Alternatives considered

- **A track-native metric format and query language.** Rejected: it would cut track off from every
  existing exporter and dashboard, and PromQL-scale scope would swallow the project.
- **Full PromQL / full Grafana schema.** Rejected for the MVP: functions and exotic panels are unbounded scope. The subset boundary is load-bearing — unknown panel types and function calls
  fail loudly so dashboards never render a guess.
- **Storing labels as first-class fields on metric records.** Rejected: it would change the Canonical
  Data Model for one feature. Name-folding keeps the model untouched and round-trips through the
  matcher parser.

## Consequences

- New dependency `prometheus/common` (plus `client_model` via expfmt) — the only outside spec
  implementation track vendors.
- `track metrics` subcommands join the CLI contract (agent-workflows): scrape, dashboard, alert.
  Domain skills (finance monitoring included) compose on top without engine changes.
- Grafana JSON files that use functions, variables, or other panel types are out of scope and say so
  at parse time.

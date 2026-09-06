# 0076. Metrics monitoring on adopted specs

Status: Accepted

## Context

track owns metric computation and visualization (ADR 0021), and market data already lands in vaults
as canonical JSONL via finance skills. What is missing is the monitoring loop around it: ingesting
outside metrics, deriving gauges, drawing dashboards, and evaluating threshold rules. In this area a
bespoke design would isolate track from the ecosystem it wants to join, so the rule here is
adopt-don't-invent: take the wire and document formats the monitoring world already shares, and map
them onto the Canonical Data Model instead of extending it.

## Decision

Three adopted surfaces, each a documented subset. Everything outside the subset is a loud error, not
a silent guess.

1. **Ingest OpenMetrics / Prometheus exposition text** (`track metrics scrape`). Parsing is the
   ecosystem's own `prometheus/common/expfmt` — track writes no exposition parser. Samples become
   `metric` records: gauge/counter values as-is (counter keeps its `_total` sample name);
   histogram/summary families flatten to their `_bucket`/`_sum`/`_count` sample names. Labels fold
   into the two fields the model has: `entity` takes the first present of `--entity-labels`
   (default `entity,symbol,instance`); every other label appends to the name in Prometheus display
   form (`name{k="v"}`). Unstamped samples take the scrape time (`--asof` overrides it).
2. **Derive gauges from any price-kind JSONL** (`track metrics derive`). The transforms are the small
   closed set finance skills already use: `change_pct`, `ma5_dev`, `ma25_dev`, `rsi14` (Wilder/RMA),
   `mom_12_1`, `high52w`. Derive never fetches and never names a source: whichever writer fills
   `data/` (track-prices today, a J-Quants writer tomorrow) feeds it unchanged, so future source
   selection is a writer choice, not a derive change.
3. **Dashboards are Grafana classic JSON, subset** (`track metrics dashboard`). Accepted: `title`,
   `panels[]` of type `timeseries` or `stat`, each target's `expr` as a metric name plus `=` label
   matchers only (no functions — derivable values are precomputed by derive), and
   `fieldConfig.defaults` `unit`/`min`/`max`/`thresholds`. The datasource convention is
   `{"type":"track","uid":"<bare data/ filename>"}`. Panels resolve to viewspec blocks
   (timeseries → line with threshold overlays; stat → the same series as a line, documented
   limitation), emitted as note Markdown for embedding — rendering stays on the existing viewspec
   path, so no new renderer is added.
4. **Alerts are Prometheus rule YAML, subset** (`track metrics alert`). Accepted per rule: `alert`,
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
- `track metrics` subcommands join the CLI contract (agent-workflows): scrape, derive, dashboard,
  alert. Finance skills compose on top without engine changes.
- Grafana JSON files that use functions, variables, or other panel types are out of scope and say so
  at parse time.

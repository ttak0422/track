# 0024. Mark + encoding View Spec

Status: Accepted

Supersedes the View Spec shape decided in ADR 0021 (chart `type` + top-level `x`/`y`/`size`). The
Canonical Data Model, renderer registry, and `track render` entry point from 0021 are unchanged.

## Context

The visualization goal is a declarative spec (Mermaid-like) from which annotated charts, rankings,
heatmaps, and composed articles are generated. The View Spec introduced in ADR 0021 keys everything off
a chart `type` (`line|bar|hbar|scatter|bubble|heatmap|timeline`) with top-level `x`, `y[]`, and `size`.

That shape does not scale to the goal. `type` bundles "what to draw" and "how it looks" into one enum,
so every visual feature multiplies against every type:

- Adding a look (color-by-field, stacking, sort/top-N, area fill) means touching *every* type's
  renderer branch.
- Adding a chart (candlestick for the `price` kind, which today has no chart at all) means
  re-implementing color, axes, filtering, and legend for that type.

Cost grows as `types × features` — a matrix. Each new expression has meant editing Go, which is the
structural gap recorded in the "track 可視化 残タスク" note.

An audit of the goal-image vocabulary (same note) split the missing expressions into: **A** — look
variations that a data→visual-channel mapping absorbs (candlestick, area, stacked/grouped, color,
shape, sort, facet); **B** — data transforms (aggregation, derived columns, resampling, joins), which
stay out of the spec per the "no computation in the spec" line (`106ad99`); **C** — expressions no
encoding can reach (free layout / scrollytelling, node-link diagrams, callout annotations, bands,
interactivity), which need their own renderer regardless. A is the bulk of the recurring Go work.

## Decision

**Adopt a Vega-Lite-style `mark` + `encoding` View Spec (schema version 2), replacing the `type`-keyed
v1.** `mark` names *what* is drawn; `encoding` maps data fields onto *visual channels*, orthogonally.

```json
{
  "version": 2,
  "title": "Price vs Index",
  "data": { "source": "metrics.jsonl", "kind": "metric" },
  "mark": "line",
  "encoding": {
    "x": { "field": "time" },
    "y": [
      { "field": "close", "title": "Close" },
      { "field": "index", "title": "Index", "axis": "y2" }
    ],
    "color": { "field": "sector" },
    "size":  { "field": "exposure" }
  }
}
```

- **Marks** (initial set, mapping the v1 types): `line`, `bar`, `point`, `area`, `rect`. Orientation,
  radius, and grid layout become mark/channel options rather than distinct types:
  - `scatter` → `mark: point`
  - `bubble` → `mark: point` + a `size` channel (no separate mark)
  - `hbar` → `mark: bar` with x/y swapped (a categorical `y`)
  - `heatmap` → `mark: rect` + a `color` (value) channel
  - `timeline` → `mark: point` on a categorical `y`
- **Channels**: `x`, `y` (one Channel or an array for multi-series), `color`, `size`, plus per-channel
  options `title`, `axis` (`y`|`y2`), `sort`, `stack`. A Channel is `{field, title?, axis?, sort?,
  stack?}`. Channels are added once and apply across all marks.
- **Orthogonality is the load-bearing property.** A channel (color, sort, stack, …) is one addition
  that works for every mark; a mark is one addition that gets every channel for free. Cost becomes
  `marks + channels` — a sum, not a product. A new mark (candlestick, later) is still Go, but it is
  small and composes with all existing channels automatically.
- **`filter` and `overlays` carry over from v1 unchanged** for now. Filter stays a spec-level select
  (row filtering, not computation). Overlays (vertical event/annotation markers) remain a separate
  concept; their richer forms (bands, horizontal reference lines, callouts) are bucket **C** and are
  designed with the future free-layout renderer, not folded into `encoding`.
- **No v1 compatibility shim.** Per the project stance (active development, breaking changes preferred
  over back-compat when they yield a better design — see CLAUDE.md), v1 is removed. The six help demo
  specs, tests, `docs/spec/visualization.md`, help text, and the `article` package's inline chart specs
  are migrated to v2. Keeping a v1→v2 translator would be exactly the speculative complexity this
  project avoids.

Scope: this ADR pivots the *structure* and rewrites the existing vocabulary onto it. It does **not** add
new marks (candlestick, area beyond the mark stub) or transforms (bucket B) or a free-layout renderer
(bucket C); those become incremental additions the new structure is meant to absorb without rework.

## Alternatives considered

- **Keep `type`, add feature flags as needed.** This is the status quo that produced the matrix. Each
  feature still fans out across every type. Rejected — it is the problem being fixed.
- **Keep v1 and translate to v2 internally.** A compat shim to avoid migrating six demos and their
  tests. Rejected as speculative: the project sanctions breaking changes, and the migration is bounded
  and mechanical.
- **Full Vega-Lite adoption (layer/facet/transform grammar now).** Rejected as premature. We take the
  mark+encoding core (which absorbs bucket A) and defer layering, faceting, and transforms until there
  is data and demand — the structure leaves room for them.

## Consequences

- `internal/track/viewspec` Spec is rewritten: `mark` + `encoding` replace `type` + top-level
  `x`/`y`/`size`. `Resolve` maps channels onto the existing `Resolved` (series/grid/points), so the
  renderers' drawing code changes little; the spec-parsing surface changes a lot.
- Both renderers (`chartjs`, `svg`) and the `article` package consume the new shape. `track render`
  help text, `docs/spec/visualization.md`, `docs/help/*`, README, and the six demo `.viewspec.json`
  files are migrated to v2.
- v1 specs stop parsing. This is acceptable: the format is young, unreleased, and only referenced by
  in-repo demos and docs.
- New look variations (color, stack, sort, …) and new marks land as isolated additions rather than
  cross-cutting edits, closing the recurring-Go-edit gap for bucket A. Buckets B and C remain open and
  are tracked separately in the "track 可視化 残タスク" note.

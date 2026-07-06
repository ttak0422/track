# 0027. Chart provenance links

Status: Accepted

## Context

The visualization goal articles tie every drawn element back to its underlying information: an event
annotation links to the news it marks, and hovering a data point shows the record behind it. track's
charts could draw all of that (overlays, tooltips — ADR 0024/0026) but nothing was traversable: labels
were plain strings, tooltips showed only series values, and clicks did nothing.

The data layer was already prepared: the canonical `event` kind carries `url` (external source) and
`note` (a vault note id) as provenance fields (ADR 0021), and `track-fetch-rss` writes `url`. The gap
was purely wiring: no View Spec vocabulary referenced those fields, the renderers dropped them, and the
frontend had no handler.

## Decision

Make the chart an index into its evidence, with the smallest vocabulary that fits ADR 0024's
channel model:

- **Three encoding channels.** `encoding.detail: [{field, title?}]` names extra record fields
  carried per datum; `encoding.href: {field}` names the field holding a datum's source URL;
  `encoding.note: {field}` the field holding a vault note id the datum references. All are
  channel-shaped (reusing `Channel`), orthogonal to marks, and restricted to the series forms
  (line/area/bar/scatter/hbar) whose resolvers align records to (label, value) slots — grid, bubble,
  and candlestick reshape records and error explicitly until they grow a slot.
- **Overlays adopt provenance automatically.** A marker record's `url` and `note` fields ride onto the
  resolved `Marker` with zero new vocabulary, because the event kind already defines them as
  provenance.
- **The option stays pure JSON.** The ECharts renderer emits extras as extra keys on data items
  (`{"value": v, "href": ..., "detail": [...]}`) and markLine items (`href`/`note`); ECharts hands an
  item's fields to event and tooltip params untouched, so no formatter functions or callbacks enter
  the option. A markLine group carrying links stops being `silent` so it accepts clicks; a linked
  series gets `cursor: pointer`.
- **One generic frontend handler.** `EChartsBlock` — the single drawing surface for every embed —
  installs one click handler: an `http(s)` `href` opens in a new tab (`noopener`; other schemes are
  ignored), else a `note` id navigates to `/notes/<id>`. When a datum carries both, the URL wins: the
  external source is the click target, and the note reference is reserved as the hover-preview target
  (a later phase, pending the shared hover-intent hook). A generic tooltip formatter renders `detail`
  rows as escaped text lines — the spec decides *what* to show, the frontend only *how*.
- **Note references are ids.** `note` carries the vault note id (not a title), so links survive
  renames without backlink rewriting. The live workspace navigates by id directly. The static export
  rewrites ids to their opaque publish slugs inside resolved `echarts` fences and **drops references
  to unpublished notes**, keeping the no-dangling-navigation policy and never leaking internal ids.
- **The SVG renderer wraps linked markers in `<a>`** (escaped, `noopener`); note references stay inert
  there — a static image has no router.

Tooltips stay text-only: links or images inside a tooltip would need the pointer to travel into a
transient hover surface (`enterable`), which is fragile and unusable on touch — the datum itself is
the click target instead.

## Consequences

- A data article's chart can now cite: markers open their news source, datums list their record's
  fields on hover and open their source on click, and both can point back into the vault.
- Everything is additive JSON: options without provenance are byte-identical to before, and old
  bundles keep working (no `href`/`note` keys → the handler does nothing).
- The `detail`/`href` channels widen `Resolved.Series` with a per-datum `Extras` slice that
  sort/limit must permute alongside `Values`; grid-form support would need an equivalent slot on
  `Grid.Cell`/`Point` and is deferred until needed.
- Hover previews of referenced notes (the third phase) wait on unifying the WikiLink / MediaFrame /
  GraphFullView hover-intent machinery into a shared hook, which charts would consume as a fourth
  surface.

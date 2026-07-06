# 0028. Marker annotation boxes

Status: Accepted

## Context

The visualization goal articles do not just make provenance reachable — they keep it visible. Every
event annotation is a small always-readable box (a date line, a wrapped headline, a source-attribution
link) laid out along the timeline, staggered into lanes when events cluster, each connected to its
position on the axis; the chart doubles as a scannable index of its evidence. After ADR 0027 track's
markers carry the underlying data (label, `url`, `note`) but surface it only on demand: a thin
markLine, a tooltip, a click. ADR 0027 also records why the tooltip can never grow into this role — a
transient hover surface cannot hold real links, especially on touch. A persistent annotation surface
is the missing piece.

It is fundamentally a layout problem: ~20 boxes must stagger without overlapping, which depends on
container width, text metrics, and the live dataZoom window — none of which the engine knows, and none
of which a pure-JSON option (ADR 0026) can compute. So the design question is where to cut the seam
between the spec, the option, and the drawing surface.

## Decision

Add an opt-in display mode that renders a marker overlay as an **annotation rail**: an always-visible
band of boxes below the plot, anchored to their x positions. The ADR 0027 split carries the seam: **the
engine decides content, membership, and order; the web frontend owns geometry.**

- **One word of vocabulary.** The `overlays[]` marker entry gains `"display": "box"` (absent = today's
  line-and-label rendering). It is validated like every overlay option — unknown values error, the
  other three overlay shapes reject it, and a form gate (the `encoding.href` pattern) restricts it to
  the category-x forms where markers anchor: line, area, bar, scatter, and candlestick; heatmap,
  timeline, bubble, and hbar error. This is the spec's first display knob, and the line it holds is:
  whether evidence is always visible is an editorial *what* (spec-authored, per chart); where each box
  lands is *how* (renderer-owned). Lane counts, widths, and placement get no vocabulary.
- **Engine-resolved box content.** `Marker` gains a `Box` flag, stamped in `Overlay.Markers` — the
  single extraction point behind all three `Resolved.Markers` fill sites. In the ECharts option each
  placeable box marker's existing markLine data item (the proven extra-key location where `href`/`note`
  already ride, pinned by tests) gains `"box": {"date": …, "host": …}`: `date` is the `at` value
  trimmed to a day by a single RFC3339 parse (else the raw string), `host` is extracted from the URL
  in Go, and non-`http(s)` hrefs are scrubbed engine-side — no renderer ever parses a URL, closing the
  scheme hole before any surface sees it. The headline is not duplicated: it stays `label.formatter`,
  the single source of truth. Box markers are emitted sorted by category index (record-order ties), so
  same-day stacks are deterministic on every surface; a marker whose `at` matches no label gets no box
  payload (skip, never fail).
- **The option keeps degrading to today's look.** The classic boxed markLine label stays on every item.
  A bare `setOption` consumer — the standalone HTML page, the composed article, a stale cached frontend
  bundle — renders exactly today's markers; the rail-drawing frontend suppresses the canvas label on
  its themed clone (never the shared cached option). Options without `display: "box"` stay
  byte-identical, extending ADR 0027's additive-JSON guarantee.
- **One generic rail in the frontend.** `EChartsBlock` — the single drawing surface for the workspace,
  the published site, `.echarts.json` embeds, and floating note windows — renders the rail as a real
  list element that is a *sibling* of the `role="img"` chart host: links become reachable by assistive
  tech, and the capture-phase wheel listener, pinch handling, and canvas re-dispatch are untouched.
  Lane assignment runs synchronously from container width plus category fractions read off the option
  itself, so the rail's height is settled before the lazily-initialized chart ever draws (no layout
  shift); `convertToPixel` then only refines anchors horizontally on `finished`/`datazoom`/resize.
  Boxes zoomed out of the window turn invisible without reflowing the lanes; overflowing the lane cap
  or a narrow container (phones, small floating windows) flips the rail to a flat list. A box shows the
  engine's date and host, the item's headline, the source link (`noopener`), and a note chip using the
  existing note-navigation call.
- **Publish safety is inherited, plus one prerequisite.** Note ids stay under the key literally named
  `note`, so the static export's generic walk keeps rewriting them to publish slugs and dropping
  unpublished references; a box whose reference was dropped renders without the chip. Prerequisite,
  shipped as its own commit first: the `.viewspec.json` asset path must route through the same rewrite —
  it currently publishes raw internal ids inside `.echarts.json`, a dead click today that an
  always-visible box would print as text.
- **SVG defers.** The SVG renderer ignores the mode and keeps today's marker rendering — an explicit,
  documented exception to ADR 0026's implemented-twice norm for this capability (the 0027 note-inert
  precedent, widened). `Box` already rides the resolved `Marker`, so a deterministic Go lane pass over
  the existing callout-box machinery can land later without any spec or option change.

## Consequences

- A data article's chart carries its evidence on the page: date, headline, and source per event,
  always visible, on the live workspace and the published site. The CLI-rendered standalone page and
  composed article keep classic markers — the goal look reaches those surfaces only via the site.
- The box payload is additive and engine-decided; tests pin that no `box` key appears without
  `display: "box"`, that box items are sorted, and that the frontend suppresses the canvas label only
  on its clone.
- Box-mode markLine items are reordered (sorted by category index) relative to record order; nothing
  observable depends on the old order, but golden-style substring tests must not assume it.
- Deferred: SVG rail boxes, hover linking between a box and its line, above-plot placement, and
  partial-overflow degradation (a "+N more" chip) — each is a follow-up that needs no new vocabulary.

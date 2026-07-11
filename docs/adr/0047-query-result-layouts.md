# 0047: Query result layouts resolve in the engine to a generated view fence

## Status

Accepted

## Context

Embedded `track-query` fences (ADR 0033) render as Markdown tables everywhere notes render. Tables
are the right default, but state-grouped boards, cover-image galleries, and date-placed calendars
are the shapes people actually reach for over the same rows (Obsidian Bases views, org-mode agenda).
The question is where layout logic lives: in each frontend, or in the engine that already runs the
query on both surfaces (live web workspace and static export).

## Decision

### Layout is an engine concern; the fence chooses it

A `:layout table|board|gallery|calendar` header argument on the `track-query` fence — the same
Org-style header arguments Babel blocks already parse — picks the shape, with `:by <column>` naming
the grouping column (board) or date column (calendar), defaulting to the first non-title column.
`table` stays the default and keeps the ADR 0033 Markdown-table path untouched.

For the other three, `query.BuildView` distributes the evaluated `Result` into named groups — board
lanes in first-appearance order (so `SORT` orders lanes), calendar days ascending by the cell's
leading `YYYY-MM-DD`, gallery rows carrying the note's cover image — and `ExpandBlocks` emits the
laid-out payload as a generated ```` ```track-view ```` fence containing one line of View JSON.
This mirrors exactly how a `viewspec` fence resolves to a ready-to-draw `echarts` option block: the
engine decides semantics (grouping, bucketing, covers, empty-result handling), the frontend only
places already-grouped rows on screen. One `QueryView` React component draws all three shapes in
both deployments; the calendar reuses the workspace calendar's grid styles. Rows link by title, so
a published view exposes exactly what a published table does.

Covers come from the surface that runs the query: the live server reads the note sidecar's `image`,
the exporter maps published cover assets, and directory sites (help/docs) lift a
`cover:: assets/<file>` inline field — the same pattern as `tags::` there.

### The web renderer preserves fence info strings

This surfaced a latent bug: both render paths sanitize the body through the web renderer *before*
fence expansion, and the renderer re-emitted fences as bare language-tagged fences, dropping header
arguments — `:layout` never reached the expander. `babel.Block` now carries the raw info string and
the web renderer round-trips it verbatim (frontends read only the first token, so this is
invisible elsewhere). The Markdown export renderer still strips header arguments; its output is
meant to be portable.

## Consequences

- All layouts are read-only by construction — the payload has no note ids or write affordances.
- An empty result renders the shared "no results" text in every layout rather than an empty board
  or month grid.
- A future layout is one `BuildView` case plus one frontend switch arm; the fence contract and
  expansion sites do not change.
- Fence header arguments now survive into web-rendered bodies for every language, which is the
  faithful behaviour but means the info string is part of the web body contract.

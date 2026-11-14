# Native graph rendering research

Research snapshot: 2026-11-12. This is an implementation decision record for the
experiment, not evidence that native graph rendering is implemented or faster.

## Decision

Start with a **single GPUI canvas, an explicit camera, and application-owned picking**.
Keep note metadata, tooltips and controls in ordinary GPUI elements outside the
camera. Reuse Go's graph data and preserve Track's existing interaction semantics.
Do not fork GPUI or adopt a third-party transform branch merely to render circles,
edges and fixed-size labels. Reconsider a general transformed subtree only if the
product needs rich, editable cards inside the graph.

## What the current workspace does

Reviewed `web/src/components/GraphCanvas.tsx`, `graphWheel.ts`, `nodeRadius.ts` and
`overviewGraph.ts` at the experiment's base `8a7ed55e`:

- Canvas 2D draws edges, then nodes, then labels. Node radii and 13px labels stay
  fixed in screen space; node area scores map to area, not squared radius.
- D3 force layout uses link/charge/centering forces. Initial settling is sliced into
  8ms budgets with a 400-tick ceiling, rather than blocking until convergence.
  A previous image remains during subsequent settling. This budget is not a measured
  rendering duration or a guarantee of 60fps.
- Empty-space drag pans; node drag temporarily pins the node and reheats its neighbors.
  A click without movement navigates. Hover emphasizes a neighborhood and opens a
  preview. Pointer cancellation releases the drag. Search highlights do not relayout.
- Full-view controls include reset and title visibility; the graph follows vault
  scope. Hover previews are transient unless explicitly pinned. Floating graph
  panels can be resized. These are parity requirements beyond the canvas itself.
- Explicit ctrl/shift-wheel zooms around the cursor; bare wheel remains page scroll.
  Touch pinch both pans and zooms. Scale is clamped to `[0.015, 4]`.
- Picking scans nodes and chooses the nearest eligible center with 5px screen-space
  padding. This is O(V) per pick. Drawing traverses nodes/edges even when offscreen.
- Full-vault overview removes unlinked nodes and retains at most 1,000 linked notes,
  prioritizing degree with deterministic ID ties. Omitted count is exposed. This is
  a readability policy, not a renderer limitation to silently remove in native UI.

Parity should preserve these behaviors before adding different layouts or scale limits.

## GPUI constraints established from primary sources

GPUI **0.2.2**, currently pinned here, provides `canvas(prepaint, paint)` and the
low-level `Window::paint_quad` / `paint_path` APIs. `Window::insert_hitbox` operates
on supplied bounds. `paint_layer` clips and manages scene ordering; it is not a
camera transform. `Svg::with_transformation` explicitly does not transform layout
or hitboxes. Therefore, a visual SVG transform is not sufficient for graph picking.

[Upstream discussion #53306](https://github.com/zed-industries/zed/discussions/53306)
proposes subtree pan/zoom with coherent hit testing. Its author reports a working
fork covering clipping, cached prepaint, hit testing and text raster scale. The
thread inspected here contains a proposal/fork and user reports, **not proof that
its API is available in the pinned release**. Do not copy its implementation or
rely on its downstream test claims as Track validation.

Primary code references:

- [GPUI canvas source](https://docs.rs/crate/gpui/0.2.2/source/src/elements/canvas.rs)
- [GPUI window painting and hitboxes](https://docs.rs/crate/gpui/0.2.2/source/src/window.rs)
- [GPUI SVG transform contract](https://docs.rs/crate/gpui/0.2.2/source/src/elements/svg.rs)
- [D3 force documentation](https://d3js.org/d3-force) and
  [many-body implementation](https://github.com/d3/d3-force/blob/main/src/manyBody.js):
  a quadtree/Barnes–Hut approximation avoids an unconditional all-pairs repulsion pass.

## Coordinate and interaction contract

Use **logical window pixels**, not physical device pixels, throughout input and
application geometry. GPUI applies display scale when painting. Include canvas
origin in every transform; a graph in a sidebar is not at the window origin.

For world position `w`, canvas origin `o`, canvas center `c`, camera pan `p`, zoom `s`:

```text
screen(w) = o + c + p + s*w
world(q)  = (q - o - c - p)/s
p_after   = q - o - c - s_after*world_before(q)
```

The last expression preserves the world point under the cursor during zoom. Clamp
zoom before calculating the new pan. Reject nonfinite values and nonpositive zoom.
A fixed screen radius `r` plus hit padding `h` becomes `(r+h)/s` for world-space
queries. Draw, pick, drag and tooltip anchoring must share this camera snapshot.

Implementation checklist:

- [ ] Paint only inside the canvas clip and ignore picks outside that viewport.
- [ ] Route graph pointer events through one canvas hitbox; do not allocate a full
  layout subtree per node. Show previews in an untransformed overlay.
- [ ] Preserve closest-center/tie behavior under overlap; require click-versus-drag
  discrimination. Clear hover on camera changes and cancel stale preview requests.
- [ ] Release pinned nodes on pointer cancellation, focus loss and window close.
- [ ] Keep text and hit radii at fixed logical-pixel size; paint labels after nodes.
- [ ] Verify native pinch event semantics rather than assuming browser ctrl-wheel
  encoding applies unchanged to GPUI's macOS events.

## Layout and large graphs

Run layout away from the UI thread, publish immutable position snapshots at bounded
intervals, and cancel by generation when the graph/vault changes. Coalesce pending
updates to the newest snapshot; never build an unbounded frame queue. Do not refit
or restart layout on every pan, hover or search highlight. Preserve positions across
small data changes, and respect the camera once the user moves it.

For the first compatible force layout, retain the current force-model intent and
fixture behavior. Evaluate a maintained Rust solver against it before selecting one;
this research does **not** nominate an untested crate or require copying D3 code.
Barnes–Hut is a candidate for repulsion, not a guarantee about the complete solver's
runtime or quality. Dense edges can still dominate O(E) work and visual clutter.

Optimize in measured stages:

1. Keep the existing overview cap and omitted-count feedback. Cache adjacency for
   hover highlighting, and repaint only on data/camera/interaction changes.
2. Cull nodes using visible world bounds expanded by fixed-screen radii. Cull edges
   by segment/viewport intersection: two offscreen endpoints can cross the viewport.
3. Add a world-space grid/quadtree for picking only when profiling justifies it.
   Camera changes need not rebuild it; moving layout snapshots do. Query by the
   largest visible hit radius, then perform the exact radius test on candidates.
4. Use zoom-dependent label limits and selected/hovered labels first. Cache text
   shaping at fixed font size; no unlimited glyph variants keyed by zoom.
5. If users need exploration beyond 1,000 nodes, prefer explicit neighborhood or
   aggregate views with drill-down over silently drawing every label/edge. A graph's
   cycles are valid; SCC aggregation would be a separate semantic view, not a
   prerequisite for a note-link graph or a replacement for its individual notes.

## Pure-geometry experiment

A deterministic Python float64 experiment (seed `53306`) generated 10,000 cameras
with zoom in `[0.015, 4]`, offsets in `[-300, 300]`, viewport centers in `[50, 1000]`,
and points in `[-1000, 1000]`. It checked camera round trips and cursor-anchored zoom,
then tested 20,000 picks at 14.99px / 15.01px around a 15px hit radius.

Result: all assertions passed; maximum round-trip error `1.22e-11`, maximum anchored
zoom error `5.44e-11`. This validates the equations in ordinary finite ranges only.
It is **not** a Rust/GPUI test, a float32 accuracy result, a high-DPI UI test or a
performance benchmark. Implement regression tests in the actual geometry module
before integrating it; include nonzero canvas origin, overlap, clip edges, invalid
coordinates and extreme zoom.

## Validation before a performance claim

Use identical synthetic graph snapshots and release builds for native/web comparisons:
empty, singleton, chain, star, disconnected clusters, cycle, sparse 1k/10k/100k stress
sets and dense subgraphs. Stress data must be opt-in; do not remove the product's
readability cap to manufacture a comparison.

Record layout-to-first-usable-frame time, p50/p95/p99 frame time during pan/zoom/drag,
input-to-selection latency, resident memory and idle CPU. Separate layout cost from
paint/label/pick cost; include warm/cold font caches, Retina scale changes and window
resizing. Test hover, drag cancellation, stale layout completion and graph replacement.

**Not yet measured:** GPUI graph frame time, GPU batching, layout convergence, picking
index speedups, large-graph memory, native gesture correctness or parity screenshots.
Do not infer any of these from GPUI using a GPU or from the existing list prototype.

## First native slice

`graph_view.rs` uses a single canvas and shared geometry transforms. It currently
uses circular placement, not force physics. Basic help-fixture drawing and picking
were verified on macOS; full graph interaction parity remains open.

A standalone release geometry harness measured 10,000 camera-plus-hit-test
iterations: 100 nodes took 5.307 ms total (0.531 µs/iteration), and 1,000 nodes
57.184 ms (5.718 µs/iteration). These exclude GPUI painting, layout, presentation,
and input latency, and are not evidence of a faster renderer than the web client.
The ignored `geometry_benchmark` test provides a repeatable CPU-only probe.

A bounded independent force solver now settles up to 1,000 nodes off the UI thread.
Generation checks discard results after replacement or dragging; larger graphs keep
all nodes in the circular fallback. Release CPU measurements for 80 iterations were
1.889 ms at 100 nodes and 156.626 ms at 1,000 nodes. This initial O(N²) solve is never
run per pointer event. Dragging still does not provide elastic neighboring movement.

## Neighborhood lookup follow-up

Hover/selection previously built all nodes' adjacency sets to obtain one node's
neighbors. A direct incident-edge scan now preserves the same available-node,
self-edge, duplicate and dangling-edge behavior without those per-node sets.
An equivalence regression covers every ID in a synthetic mixed-edge graph.

Standalone optimized Rust measurements on Apple Silicon, 2026-11-14, used 200
serial lookups per case; both implementations consumed identical graph inputs.

| Nodes | Edges | Full adjacency, µs/lookup | Direct scan, µs/lookup |
| ---: | ---: | ---: | ---: |
| 100 | 99 | 17.440 | 1.753 |
| 100 | 490 | 63.170 | 2.031 |
| 1,000 | 999 | 182.283 | 6.675 |
| 1,000 | 49,900 | 6,562.307 | 34.109 |

The ignored `neighborhood_benchmark` test retains the old lookup as a repeatable
CPU-only baseline. These are single-run microbenchmarks, not frame-time, input
latency or GPU measurements, and do not establish native/web performance parity.

Run the retained comparison after dependency preparation:

```sh
cargo test --locked --release --manifest-path experiments/gpui/Cargo.toml \
  neighborhood_benchmark -- --ignored --nocapture --test-threads=1
```

## Full-vault Graph prerequisite (2026-11-14)

The rail's **Full graph** opens one ordinary Graph tab, preserving note tabs.
It uses the owned `--server-port` session and selected working-vault scope, via
read-only `/api/graph`. CLI/snapshot modes explicitly report this requirement.
The existing local aside remains separate. Connected-node degree selection draws
at most 1,000 nodes, reports hidden notes including isolates, and does not cap
server evaluation or response size. Empty linked graphs are explicit, as are fetch
errors and partial-read diagnostics. Titles toggle independently of filtering;
reset fits the current native positions without refetching or changing titles.
Node click opens the captured graph vault identity. Existing Save/Discard/Cancel
navigation guards apply; request, scope, launch and storage checks reject stale
completions. Graph is not recorded as a note visit.

This is not full parity: bounded mouse-hover previews and one explicit pin are now integrated (see GRAPH-PREVIEW-FOUNDATION.md); positions use the
existing bounded force snapshot/circular fallback rather than reference physics,
number grouping currently uses commas rather than OS locale, and native GUI,
high-DPI, gesture, accessibility and timing behavior are unverified. Automated
synthetic model/wire tests do not establish visual or performance parity.

## Camera and label compatibility (2026-11-14)

Unmodified wheel input is no longer consumed as graph panning, matching the Web
surface's surrounding-scroll behavior. Explicit Ctrl/Shift wheel still zooms,
and background dragging still pans. A separate camera-intent revision prevents
a late background force snapshot from auto-fitting over newer pan, zoom or Reset
input; the accepted layout itself is retained. Graph replacement continues to
invalidate old layout completions through its existing generation guard.

Three pure policy tests cover untouched/changed camera intent and wheel modifiers;
compilation covers the native event wiring. Real wheel propagation, native pinch,
pointer capture and visual camera behavior remain GUI-unverified. No performance
improvement is claimed for this correctness change.

Native titles now use screen-space density rules: a canvas with either dimension
at least 420 logical pixels shows ordinary titles from scale 0.26, or high-degree
nodes from degree four. Small canvases retain center/highlight titles only; the
titles toggle disables all labels. Labels use fixed 13px text, a panel backdrop,
and shortened non-hover titles with an ID fallback. Truncation preserves Unicode
scalars, not grapheme clusters. Hover retains the complete title. Size changes
invalidate label layout without restarting force placement.

Two synthetic tests cover size/zoom/degree boundaries, title suppression,
highlight exceptions and Unicode title fallback/shortening. Native text alignment,
hover transfer, clipping, theme contrast and accessibility remain GUI-unverified.
The backdrop is opaque rather than the Web canvas's translucent fill. No frame,
GPU or input-latency measurements were performed; fewer labels are not by themselves
evidence of a rendering speedup.

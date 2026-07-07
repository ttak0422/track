import { useLayoutEffect, useRef, useState } from "react";

// One annotation box, read off a box-mode markLine item (ADR 0028). The engine already resolved the
// content — the date line, the source host, the sort order — so the rail only decides geometry.
// A box carries no vault-note affordance: fetched event data only ever provides a source URL, so
// the box surfaces the external source and note refs stay a line-click concern (ADR 0027).
export interface RailBox {
  at: string;
  date: string;
  headline: string;
  href: string;
  host: string;
}

export const BOX_WIDTH = 160;
export const LANE_HEIGHT = 76;
const RAIL_TOP = 10;
const LANE_GAP = 8;
const MAX_LANES = 6;
const MIN_RAIL_WIDTH = 560;
// The plot's horizontal inset is unknown before the chart instance measures itself; a rough guess
// places the estimated anchors, and convertToPixel nudges them once the canvas has drawn.
const PLOT_INSET = 24;

// extractRail pulls the annotation boxes out of a ready-to-draw option: every markLine data item
// carrying the engine's "box" payload, in emitted (category-sorted) order, plus each box's fraction
// along the category axis — enough to lay the rail out before the chart instance exists.
export function extractRail(option: Record<string, unknown>): {
  boxes: RailBox[];
  fractions: number[];
} {
  const labels = categoryLabels(option);
  const boxes: RailBox[] = [];
  const fractions: number[] = [];
  const series = Array.isArray(option.series) ? option.series : [];
  for (const s of series) {
    const markLine = (s as { markLine?: { data?: unknown } }).markLine;
    if (!Array.isArray(markLine?.data)) {
      continue;
    }
    for (const raw of markLine.data) {
      const item = raw as {
        xAxis?: unknown;
        box?: unknown;
        label?: { formatter?: unknown };
        href?: unknown;
        note?: unknown;
      };
      if (typeof item.box !== "object" || item.box === null) {
        continue;
      }
      const at = typeof item.xAxis === "string" ? item.xAxis : "";
      const index = labels.indexOf(at);
      if (index < 0) {
        continue; // the engine skips unplaceable markers; stay defensive on foreign options
      }
      const payload = item.box as { date?: unknown; host?: unknown };
      boxes.push({
        at,
        date: typeof payload.date === "string" ? payload.date : at,
        headline: typeof item.label?.formatter === "string" ? item.label.formatter : "",
        // The engine scrubs non-http(s) sources; re-check here so a foreign option cannot smuggle a
        // scheme through the rail's real <a> elements.
        href: typeof item.href === "string" && /^https?:\/\//i.test(item.href) ? item.href : "",
        host: typeof payload.host === "string" ? payload.host : "",
      });
      fractions.push((index + 0.5) / labels.length);
    }
  }
  return { boxes, fractions };
}

function categoryLabels(option: Record<string, unknown>): string[] {
  const first = Array.isArray(option.xAxis) ? option.xAxis[0] : option.xAxis;
  const data = (first as { data?: unknown } | undefined)?.data;
  return Array.isArray(data) ? data.filter((l): l is string => typeof l === "string") : [];
}

export interface RailSlot {
  left: number;
  lane: number;
}

// layoutLanes staggers boxes (already in x order) into the lowest lane with room, exactly the
// greedy first-fit the goal look needs; the caller flips the whole rail to a flat list when the
// lanes overflow or the container is too narrow for absolute placement.
export function layoutLanes(xs: number[], width: number): { slots: RailSlot[]; lanes: number } {
  const laneRight: number[] = [];
  const slots: RailSlot[] = [];
  for (const x of xs) {
    const left = clampLeft(x, width);
    let lane = laneRight.findIndex((right) => left >= right + LANE_GAP);
    if (lane < 0) {
      lane = laneRight.length;
    }
    laneRight[lane] = left + BOX_WIDTH;
    slots.push({ left, lane });
  }
  return { slots, lanes: laneRight.length };
}

function clampLeft(x: number, width: number): number {
  return Math.min(Math.max(x - BOX_WIDTH / 2, 0), Math.max(0, width - BOX_WIDTH));
}

export interface RailAnchors {
  // Refined pixel anchor per box; a null entry is a box outside the current dataZoom window
  // (hidden without restacking the lanes).
  xs: (number | null)[];
  // Distances from the chart container's bottom/top edge to the plot's own edge: the stems span
  // them so each box's line visually continues the marker line instead of breaking at the axis
  // labels (below) or the legend strip (above).
  gapBelow: number;
  gapAbove: number;
}

export type RailSide = "above" | "below";

// railSide alternates boxes between the two rails (even emitted index below, odd above), so dense
// timelines split their depth across the chart like the goal articles, and a same-day pair lands on
// opposite sides of its shared anchor.
export function railSide(index: number): RailSide {
  return index % 2 === 0 ? "below" : "above";
}

interface MarkerRailProps {
  boxes: RailBox[];
  fractions: number[];
  // Measured geometry from the chart instance; null means "not measured yet" (estimates place the
  // boxes until the canvas has drawn).
  anchors: RailAnchors | null;
  side: RailSide;
}

// MarkerRail renders one side of the box-mode markers as an always-visible evidence band hugging
// the chart: boxes alternate above and below it (railSide), so dense timelines split their depth
// across the plot like the goal articles. Each rail is a real list OUTSIDE the role="img" chart
// host, so its links are reachable (keyboard and assistive tech) and the chart's wheel/pinch
// handling is untouched. Lane geometry is frozen at the full category range: each rail's height is
// settled from the option alone — before the lazily initialized canvas ever draws — and zoom
// gestures translate or hide boxes without ever changing it (no layout shift, no mid-drag reflow).
// When either side's lanes overflow (or the container is narrow), the "below" instance renders
// every box as one flat list and the "above" instance disappears — a single degradation mechanism.
export function MarkerRail({ boxes, fractions, anchors, side }: MarkerRailProps) {
  const ref = useRef<HTMLOListElement>(null);
  const [width, setWidth] = useState(0);

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) {
      return;
    }
    setWidth(el.clientWidth);
    if (typeof ResizeObserver === "undefined") {
      return;
    }
    const ro = new ResizeObserver(() => setWidth(el.clientWidth));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  const span = Math.max(0, width - PLOT_INSET * 2);
  const fullXs = fractions.map((f) => PLOT_INSET + f * span);
  // Both instances lay out both sides from the same inputs, so their mode decision always agrees.
  const sideIndexes = (s: RailSide) => boxes.map((_, i) => i).filter((i) => railSide(i) === s);
  const mine = sideIndexes(side);
  const laneCount = (s: RailSide) =>
    layoutLanes(
      sideIndexes(s).map((i) => fullXs[i]),
      width,
    ).lanes;
  const list = width < MIN_RAIL_WIDTH || laneCount("below") > MAX_LANES || laneCount("above") > MAX_LANES;

  const content = (b: RailBox) => (
    <>
      <time className="chart-annotation-date">{b.date}</time>
      {b.headline !== "" && <span className="chart-annotation-text">{b.headline}</span>}
      {b.href !== "" && (
        <span className="chart-annotation-links">
          <a href={b.href} target="_blank" rel="noopener noreferrer">
            {b.host !== "" ? b.host : "source"} ↗
          </a>
        </span>
      )}
    </>
  );

  // In list mode every box stacks in the single below-chart list; the above instance renders an
  // empty rail. It must stay mounted either way: the element is what carries the width measurement
  // (the first render always measures 0), so unmounting would freeze the mode decision.
  if (list || mine.length === 0) {
    const stacked = list && side === "below" ? boxes : [];
    return (
      <ol
        ref={ref}
        className="chart-annotations"
        data-mode={stacked.length > 0 ? "list" : "empty"}
        aria-label="Chart annotations"
      >
        {stacked.map((b, i) => (
          <li key={i} className="chart-annotation">
            {content(b)}
          </li>
        ))}
      </ol>
    );
  }
  const { slots, lanes } = layoutLanes(
    mine.map((i) => fullXs[i]),
    width,
  );
  const gap = side === "below" ? (anchors?.gapBelow ?? 0) : (anchors?.gapAbove ?? 0);
  return (
    <ol
      ref={ref}
      className="chart-annotations"
      data-mode="rail"
      data-side={side}
      aria-label="Chart annotations"
      style={{ height: lanes * LANE_HEIGHT + RAIL_TOP }}
    >
      {mine.map((boxIndex, j) => {
        const b = boxes[boxIndex];
        const x = anchors === null ? fullXs[boxIndex] : anchors.xs[boxIndex];
        if (x === null) {
          return null; // zoomed out of the window; lanes stay put, the box just disappears
        }
        const left = clampLeft(x, width);
        // Lane 0 hugs the chart on both sides: downward-growing lanes below, upward-growing above.
        const top =
          side === "below"
            ? slots[j].lane * LANE_HEIGHT + RAIL_TOP
            : (lanes - 1 - slots[j].lane) * LANE_HEIGHT;
        // The stem continues the marker line across the chart's inset (axis labels below, the
        // legend strip above) up to the plot itself. It is drawn from the box's own top edge and
        // stacked behind every box (negative z-index), so the segment overlapping any box — its own
        // or a nearer lane's — stays hidden behind the text instead of striking through it.
        const stemTop = side === "below" ? -(gap + top) : top;
        const stemHeight =
          side === "below" ? gap + top + 2 : lanes * LANE_HEIGHT + RAIL_TOP - top + gap;
        return (
          <li key={boxIndex} className="chart-annotation" style={{ left, top, width: BOX_WIDTH }}>
            <span
              className="chart-annotation-stem"
              aria-hidden
              style={{
                left: Math.min(Math.max(x - left, 4), BOX_WIDTH - 4),
                top: side === "below" ? stemTop : 0,
                height: stemHeight,
              }}
            />
            {content(b)}
          </li>
        );
      })}
    </ol>
  );
}

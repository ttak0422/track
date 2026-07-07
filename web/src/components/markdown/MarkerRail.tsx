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
export const RAIL_TOP = 10;
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

export type RailSide = "above" | "below";

// railSide alternates boxes between the two rails (even emitted index below, odd above), so dense
// timelines split their depth across the chart like the goal articles, and a same-day pair lands on
// opposite sides of its shared anchor.
export function railSide(index: number): RailSide {
  return index % 2 === 0 ? "below" : "above";
}

export interface SideLayout {
  indexes: number[]; // box indexes on this side, in emitted (category-sorted) order
  slots: RailSlot[]; // one per entry of indexes
  lanes: number;
  height: number; // the band's total height; 0 when the side is empty
}

export interface RailLayout {
  mode: "rail" | "list";
  fullXs: number[]; // full-range anchor estimate per box (all boxes, both sides)
  above: SideLayout;
  below: SideLayout;
}

// computeRailLayout settles the whole rail geometry from the option and the container width alone —
// no chart instance — so band heights (and the grid-top shift the above band needs) are known before
// the lazily initialized canvas ever draws. Lane assignment is frozen at the full category range:
// zoom gestures only translate or hide boxes, never restack them.
export function computeRailLayout(
  boxCount: number,
  fractions: number[],
  width: number,
): RailLayout {
  const span = Math.max(0, width - PLOT_INSET * 2);
  const fullXs = fractions.map((f) => PLOT_INSET + f * span);
  const sideOf = (s: RailSide): SideLayout => {
    const indexes = Array.from({ length: boxCount }, (_, i) => i).filter(
      (i) => railSide(i) === s,
    );
    const { slots, lanes } = layoutLanes(
      indexes.map((i) => fullXs[i]),
      width,
    );
    return {
      indexes,
      slots,
      lanes,
      height: indexes.length === 0 ? 0 : lanes * LANE_HEIGHT + RAIL_TOP,
    };
  };
  const above = sideOf("above");
  const below = sideOf("below");
  const mode =
    width < MIN_RAIL_WIDTH || above.lanes > MAX_LANES || below.lanes > MAX_LANES
      ? "list"
      : "rail";
  return { mode, fullXs, above, below };
}

export interface RailAnchors {
  // Refined pixel anchor per box; a null entry is a box outside the current dataZoom window
  // (hidden without restacking the lanes).
  xs: (number | null)[];
  // Distance from the chart container's bottom edge up to the plot's bottom edge (the axis-label /
  // zoom-slider strip): the below band's stems span it so each box's line continues the marker line.
  gapBelow: number;
}

interface MarkerRailProps {
  boxes: RailBox[];
  layout: RailLayout;
  // Measured geometry from the chart instance; null means "not measured yet" (estimates place the
  // boxes until the canvas has drawn).
  anchors: RailAnchors | null;
  side: RailSide;
  width: number;
  // The above band overlays the chart between its legend and the (pushed-down) plot; the host
  // positions it at the option's original grid top.
  offsetTop?: number;
}

// MarkerRail renders one side of the box-mode markers as an always-visible evidence band: the below
// band sits under the chart, the above band in a strip reserved between the legend and the plot
// (the host grows the chart and shifts grid.top by its height). Each band is a real list OUTSIDE
// the role="img" chart host, so its links are reachable (keyboard and assistive tech) and the
// chart's wheel/pinch handling is untouched. When lanes overflow or the container is narrow, the
// below instance renders every box as one flat list and the above instance disappears — a single
// degradation mechanism.
export function MarkerRail({ boxes, layout, anchors, side, width, offsetTop }: MarkerRailProps) {
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

  if (layout.mode === "list") {
    if (side === "above") {
      return null; // in list mode every box stacks in the single below-chart list
    }
    return (
      <ol className="chart-annotations" data-mode="list" aria-label="Chart annotations">
        {boxes.map((b, i) => (
          <li key={i} className="chart-annotation">
            {content(b)}
          </li>
        ))}
      </ol>
    );
  }
  const mine = side === "below" ? layout.below : layout.above;
  if (mine.indexes.length === 0) {
    return null;
  }
  const { slots, lanes } = mine;
  const gapBelow = anchors?.gapBelow ?? 0;
  return (
    <ol
      className="chart-annotations"
      data-mode="rail"
      data-side={side}
      aria-label="Chart annotations"
      style={side === "above" ? { top: offsetTop ?? 0, height: mine.height } : { height: mine.height }}
    >
      {mine.indexes.map((boxIndex, j) => {
        const b = boxes[boxIndex];
        const x = anchors === null ? layout.fullXs[boxIndex] : anchors.xs[boxIndex];
        if (x === null) {
          return null; // zoomed out of the window; lanes stay put, the box just disappears
        }
        const left = clampLeft(x, width);
        // Lane 0 hugs the plot on both sides: downward-growing lanes below, upward-growing above.
        const top =
          side === "below"
            ? slots[j].lane * LANE_HEIGHT + RAIL_TOP
            : (lanes - 1 - slots[j].lane) * LANE_HEIGHT;
        // The stem continues the marker line to the plot's edge. Below, it also spans the measured
        // axis-label strip; above, the band's own bottom edge IS the plot top (the host reserved the
        // strip), so it just runs to the band's end. Stems stack behind every box (negative z-index)
        // and behind the canvas (the chart host paints over the bands' stems), so a line never
        // strikes through box text or axis labels.
        const stemTop = side === "below" ? -(gapBelow + top) : top;
        const stemHeight =
          side === "below" ? gapBelow + top + 2 : mine.height - top;
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

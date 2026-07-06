import { Link } from "@tanstack/react-router";
import { useLayoutEffect, useRef, useState } from "react";
import { useNoteQuery } from "../../queries";

// One annotation box, read off a box-mode markLine item (ADR 0028). The engine already resolved the
// content — the date line, the source host, the sort order — so the rail only decides geometry.
export interface RailBox {
  at: string;
  date: string;
  headline: string;
  href: string;
  host: string;
  note: string;
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
        note: typeof item.note === "string" ? item.note : "",
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
  // Distance from the chart container's bottom edge up to the plot's bottom edge: the stems span it
  // so each box's line visually continues the marker line instead of breaking at the axis labels.
  gap: number;
}

interface MarkerRailProps {
  boxes: RailBox[];
  fractions: number[];
  // Measured geometry from the chart instance; null means "not measured yet" (estimates place the
  // boxes until the canvas has drawn).
  anchors: RailAnchors | null;
}

// NoteRef renders a box's vault-note reference exactly like a note link anywhere else in the reader:
// the referenced note's title as a wiki link. The title comes from the note endpoint (cached and
// shared with previews); until it arrives — or if the reference cannot load — a neutral "note" label
// keeps the link usable.
function NoteRef({ noteID }: { noteID: string }) {
  const note = useNoteQuery(noteID);
  const title = note.data?.note.title ?? "";
  return (
    <Link className="wiki-link" to="/notes/$noteId" params={{ noteId: noteID }}>
      {title !== "" ? title : "note"}
    </Link>
  );
}

// MarkerRail renders box-mode markers as an always-visible evidence band below the chart. It is a
// real list OUTSIDE the role="img" chart host, so its links are reachable (keyboard and assistive
// tech) and the chart's wheel/pinch handling is untouched. Lane geometry is frozen at the full
// category range: the rail's height is settled from the option alone — before the lazily
// initialized canvas ever draws — and zoom gestures translate or hide boxes without ever changing
// it (no layout shift, no mid-drag reflow).
export function MarkerRail({ boxes, fractions, anchors }: MarkerRailProps) {
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
  const { slots, lanes } = layoutLanes(fullXs, width);
  const mode = width < MIN_RAIL_WIDTH || lanes > MAX_LANES ? "list" : "rail";

  const content = (b: RailBox) => (
    <>
      <time className="chart-annotation-date">{b.date}</time>
      {b.headline !== "" && <span className="chart-annotation-text">{b.headline}</span>}
      {(b.href !== "" || b.note !== "") && (
        <span className="chart-annotation-links">
          {b.href !== "" && (
            <a href={b.href} target="_blank" rel="noopener noreferrer">
              {b.host !== "" ? b.host : "source"} ↗
            </a>
          )}
          {b.note !== "" && <NoteRef noteID={b.note} />}
        </span>
      )}
    </>
  );

  if (mode === "list") {
    return (
      <ol ref={ref} className="chart-annotations" data-mode="list" aria-label="Chart annotations">
        {boxes.map((b, i) => (
          <li key={i} className="chart-annotation">
            {content(b)}
          </li>
        ))}
      </ol>
    );
  }
  return (
    <ol
      ref={ref}
      className="chart-annotations"
      data-mode="rail"
      aria-label="Chart annotations"
      style={{ height: lanes * LANE_HEIGHT + RAIL_TOP }}
    >
      {boxes.map((b, i) => {
        const x = anchors === null ? fullXs[i] : anchors.xs[i];
        if (x === null) {
          return null; // zoomed out of the window; lanes stay put, the box just disappears
        }
        const left = clampLeft(x, width);
        const top = slots[i].lane * LANE_HEIGHT + RAIL_TOP;
        // The stem continues the marker line: it reaches up past the rail's top edge and across the
        // chart's bottom inset (axis labels, zoom slider) to the plot itself, so the line reads as
        // one unbroken drop from the plot into the box.
        const stem = top + (anchors?.gap ?? 0);
        return (
          <li key={i} className="chart-annotation" style={{ left, top, width: BOX_WIDTH }}>
            <span
              className="chart-annotation-stem"
              aria-hidden
              style={{ left: Math.min(Math.max(x - left, 4), BOX_WIDTH - 4), top: -stem, height: stem }}
            />
            {content(b)}
          </li>
        );
      })}
    </ol>
  );
}

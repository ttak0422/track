import { useNavigate } from "@tanstack/react-router";
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { CodeBlock } from "./CodeBlock";
import { applyChartTheme, chartThemeFromCSS } from "./echartsTheme";
import { computeRailLayout, extractRail, MarkerRail, type RailAnchors } from "./MarkerRail";
import { useThemeVersion } from "./MermaidDiagram";

// The chart container's CSS height (styles.css .viewspec-chart); the above annotation band grows the
// container by its own height so the plot keeps this size while grid.top shifts down.
const CHART_BASE_HEIGHT = 360;

// echartsModule caches the lazy import so every chart on a page shares one load. ECharts is pulled in
// on demand (a separate chunk): pages without charts never download it. Both the live workspace and
// the static site render through this module — the option always arrives pre-resolved (from the
// server endpoint live, or baked in at export time statically), so no chart resolution happens here.
let echartsModule: Promise<typeof import("echarts")> | undefined;
function loadECharts() {
  echartsModule ??= import("echarts");
  return echartsModule;
}

interface EChartsBlockProps {
  option: Record<string, unknown>;
}

// EChartsBlock draws a ready-to-draw ECharts option into a sized container. It is the single drawing
// surface behind every chart embed: fenced ```viewspec blocks in the live workspace (ViewSpecChart),
// fenced ```echarts blocks and .echarts.json asset embeds in the published static site. The instance
// is kept across option updates (live re-renders apply in place via setOption) and disposed only on
// unmount.
export function EChartsBlock({ option }: EChartsBlockProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const visible = useVisible(containerRef);
  const navigate = useNavigate();
  // Redraw with the new colors when the app theme flips; the option itself is theme-neutral and
  // recolored at draw time (applyChartTheme).
  const themeVersion = useThemeVersion();
  // Box-mode markers (ADR 0028) render as annotation bands hugging the chart. The boxes and their
  // full-range positions come straight off the option, so the bands lay out (and reserve their
  // heights) before the chart instance exists; the instance later refines the pixel anchors.
  const rail = useMemo(() => extractRail(option), [option]);
  const [anchors, setAnchors] = useState<RailAnchors | null>(null);
  useEffect(() => setAnchors(null), [option]);
  const [width, setWidth] = useState(0);
  useLayoutEffect(() => {
    const el = containerRef.current;
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
  const layout = useMemo(
    () => computeRailLayout(rail.boxes.length, rail.fractions, width),
    [rail, width],
  );
  // The above band lives between the legend and the plot: the chart grows by the band's height and
  // grid.top shifts down the same amount (applyRailChrome), so the plot keeps its size.
  const aboveHeight = layout.mode === "rail" ? layout.above.height : 0;
  const baseGridTop = gridTopOf(option);

  useEffect(() => {
    const el = containerRef.current;
    if (!el || !visible) {
      return;
    }
    let disposed = false;
    let chart: import("echarts").ECharts | undefined;
    let observer: ResizeObserver | undefined;
    void loadECharts().then((echarts) => {
      if (disposed || !containerRef.current) {
        return;
      }
      // getInstanceByDom keeps the existing instance across live updates, so setOption transitions
      // smoothly instead of tearing the chart down.
      chart = echarts.getInstanceByDom(el) ?? echarts.init(el);
      const theme = chartThemeFromCSS();
      const themed = applyChartTheme(option, theme);
      attachDetailTooltip(themed);
      suppressBoxLabels(themed);
      applyRailChrome(themed, {
        aboveHeight,
        labelChip: layout.mode === "rail" && layout.below.indexes.length > 0,
        labelBackground: theme.panel,
      });
      chart.setOption(themed, { notMerge: true });
      // A datum can carry provenance the Go engine put on it (see the View Spec's encoding.href and
      // the overlay event url/note): clicking opens the source URL, or navigates to the referenced
      // vault note when there is no URL. off() first — the instance survives option updates, and the
      // handler must not stack.
      chart.off("click");
      chart.on("click", (params: unknown) => {
        const data = (params as { data?: { href?: unknown; note?: unknown } }).data;
        const href = typeof data?.href === "string" ? data.href : "";
        const note = typeof data?.note === "string" ? data.note : "";
        if (/^https?:\/\//i.test(href)) {
          window.open(href, "_blank", "noopener,noreferrer");
          return;
        }
        if (note !== "") {
          void navigate({ to: "/notes/$noteId", params: { noteId: note } });
        }
      });
      // Refine each annotation box's anchor to the true axis pixel, and hide boxes whose marker sits
      // outside the current dataZoom window. The rail's lanes are frozen at the full range, so this
      // only ever slides or hides boxes — the rail's height never moves during a gesture.
      const layoutAnchors = () => {
        if (!chart || rail.boxes.length === 0) {
          return;
        }
        const midY = el.clientHeight / 2;
        const xs = rail.boxes.map((b) => {
          const x = chart!.convertToPixel({ xAxisIndex: 0 }, b.at);
          return Number.isFinite(x) && chart!.containPixel({ gridIndex: 0 }, [x, midY])
            ? x
            : null;
        });
        setAnchors({ xs, gapBelow: plotBottomGap(chart!, xs, el.clientHeight, midY) });
      };
      if (rail.boxes.length > 0) {
        layoutAnchors();
        chart.off("datazoom");
        chart.on("datazoom", layoutAnchors);
      }
      if (typeof ResizeObserver !== "undefined") {
        observer = new ResizeObserver(() => {
          chart?.resize();
          layoutAnchors();
        });
        observer.observe(el);
      }
    });
    return () => {
      disposed = true;
      observer?.disconnect();
    };
  }, [option, visible, themeVersion, navigate, rail, layout, aboveHeight]);

  // Dispose the ECharts instance only on unmount; the option effect above reuses it across updates.
  useEffect(() => {
    const el = containerRef.current;
    return () => {
      if (el) {
        void loadECharts().then((echarts) => echarts.getInstanceByDom(el)?.dispose());
      }
    };
  }, []);

  // A plain wheel over the chart must keep scrolling the page, but zrender's canvas listener swallows
  // every wheel event once an inside dataZoom exists — even with its zoom gated behind Shift. Stop
  // plain wheels in the capture phase so zrender never sees them (the browser default, scrolling, still
  // runs); Shift+wheel passes through and zooms, matching the option's zoomOnMouseWheel: "shift".
  //
  // A trackpad pinch arrives as a ctrl+wheel event (every engine's convention). The option's zoom gate
  // accepts a single key, so translate the pinch into the Shift+wheel the chart understands and keep
  // the original from the browser, whose default for ctrl+wheel is zooming the whole page.
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const onWheel = (event: WheelEvent) => {
      if (event.ctrlKey) {
        event.preventDefault();
        event.stopPropagation();
        const synthetic = new WheelEvent("wheel", {
          deltaY: event.deltaY,
          deltaX: event.deltaX,
          clientX: event.clientX,
          clientY: event.clientY,
          shiftKey: true,
          bubbles: true,
          cancelable: true,
        });
        // Chromium gives a constructor-built WheelEvent a wheelDelta equal to deltaY — the opposite
        // sign of a real event's wheelDelta (−3×deltaY) — so zrender read the pinch backwards
        // (pinch-out zoomed out). Zero it so zrender's own deltaY polyfill derives the sign instead.
        Object.defineProperty(synthetic, "wheelDelta", { value: 0 });
        el.querySelector("canvas")?.dispatchEvent(synthetic);
        return;
      }
      if (!event.shiftKey) event.stopPropagation();
    };
    el.addEventListener("wheel", onWheel, { capture: true, passive: false });
    return () => el.removeEventListener("wheel", onWheel, { capture: true });
  }, []);

  const chartHost = (
    <div
      ref={containerRef}
      className="viewspec-chart"
      role="img"
      aria-label="Chart"
      style={aboveHeight > 0 ? { height: CHART_BASE_HEIGHT + aboveHeight } : undefined}
    />
  );
  if (rail.boxes.length === 0) {
    return chartHost;
  }
  // The bands are siblings of the role="img" host (not children): their links stay visible to
  // assistive tech, and the wheel/pinch listeners above keep their scope. Boxes alternate between
  // the above and below bands (railSide); the above band overlays the strip reserved between the
  // legend and the plot (offsetTop = the option's original grid top).
  return (
    <figure className="viewspec-chart-wrap">
      {chartHost}
      <MarkerRail
        boxes={rail.boxes}
        layout={layout}
        anchors={anchors}
        side="above"
        width={width}
        offsetTop={baseGridTop}
      />
      <MarkerRail boxes={rail.boxes} layout={layout} anchors={anchors} side="below" width={width} />
    </figure>
  );
}

// gridTopOf reads the option's grid.top (the plot's top edge — where the above annotation band
// starts). The engine emits it for every legend-bearing chart; 60 is ECharts' own default for the
// rest (candlestick).
function gridTopOf(option: Record<string, unknown>): number {
  const grid = option.grid as { top?: unknown } | undefined;
  return typeof grid?.top === "number" ? grid.top : 60;
}

// applyRailChrome adjusts the drawn clone for the annotation bands: the plot slides down by the
// above band's height (the container grew the same amount, so the plot keeps its size), and the
// x-axis labels get an opaque chip so a stem passing behind them never muddies the text. Clone
// only — the option itself stays consumer-agnostic.
export function applyRailChrome(
  option: Record<string, unknown>,
  opts: { aboveHeight: number; labelChip: boolean; labelBackground: string },
): void {
  if (opts.aboveHeight > 0) {
    const grid = (option.grid as Record<string, unknown> | undefined) ?? {};
    option.grid = { ...grid, top: gridTopOf(option) + opts.aboveHeight };
  }
  if (opts.labelChip) {
    for (const axis of Array.isArray(option.xAxis) ? option.xAxis : [option.xAxis]) {
      if (typeof axis !== "object" || axis === null) {
        continue;
      }
      const a = axis as Record<string, unknown>;
      a.axisLabel = {
        ...(a.axisLabel as Record<string, unknown> | undefined),
        backgroundColor: opts.labelBackground,
        padding: [1, 3],
        borderRadius: 2,
      };
    }
  }
}

// plotBottomGap measures how far the plot's bottom edge sits above the chart container's bottom —
// the strip holding the axis labels and zoom slider — by bisecting containPixel along a visible
// marker's x. The below band's stems span that strip so each box's line continues the marker line
// unbroken (the above band needs no measurement: its bottom edge is the plot top by construction).
// Without a visible anchor (everything zoomed out) there is nothing to bridge.
export function plotBottomGap(
  chart: import("echarts").ECharts,
  xs: (number | null)[],
  height: number,
  insideY: number,
): number {
  const x = xs.find((v): v is number => v !== null);
  if (x === undefined || !chart.containPixel({ gridIndex: 0 }, [x, insideY])) {
    return 0;
  }
  let inside = insideY;
  let outside = height;
  for (let i = 0; i < 10; i++) {
    const mid = (inside + outside) / 2;
    if (chart.containPixel({ gridIndex: 0 }, [x, mid])) {
      inside = mid;
    } else {
      outside = mid;
    }
  }
  return Math.max(0, Math.round(height - inside));
}

// suppressBoxLabels hides the classic in-plot label on box-mode markLine items — the rail shows the
// text instead, and double-rendering it on the canvas would shout. It runs on the themed clone only:
// the option itself keeps the label so bare-setOption consumers (standalone page, composed article)
// keep today's marker look.
export function suppressBoxLabels(option: Record<string, unknown>): void {
  const series = Array.isArray(option.series) ? option.series : [];
  for (const s of series) {
    const data = (s as { markLine?: { data?: unknown } }).markLine?.data;
    if (!Array.isArray(data)) {
      continue;
    }
    for (const item of data) {
      if (typeof item === "object" && item !== null && "box" in item) {
        (item as Record<string, unknown>).label = { show: false };
      }
    }
  }
}

// A tooltip param's shape, narrowed to what the detail formatter reads. detail rows come from the
// View Spec's encoding.detail channel, carried on each data item by the Go engine.
interface TooltipParam {
  name?: string;
  axisValueLabel?: string;
  seriesName?: string;
  marker?: string;
  value?: unknown;
  data?: { detail?: unknown };
}

// attachDetailTooltip installs a generic tooltip formatter when any datum carries detail rows: the
// default series/value lines plus one "label: value" line per detail field. What to show is decided
// by the spec (Go side); this only renders it. Charts without detail keep the ECharts default.
export function attachDetailTooltip(option: Record<string, unknown>): void {
  if (!chartHasDetail(option)) {
    return;
  }
  const tooltip = (option.tooltip as Record<string, unknown> | undefined) ?? {};
  option.tooltip = { ...tooltip, formatter: detailTooltipFormatter };
}

function chartHasDetail(option: Record<string, unknown>): boolean {
  const series = Array.isArray(option.series) ? option.series : [];
  return series.some((s) => {
    const data = (s as { data?: unknown }).data;
    return (
      Array.isArray(data) &&
      data.some((d) => typeof d === "object" && d !== null && "detail" in d)
    );
  });
}

// detailTooltipFormatter renders both trigger shapes (axis = array of params, item = one param):
// header, then per-series value lines, then the datum's detail rows. Values come from note data, so
// everything is HTML-escaped; the marker span is ECharts-generated and safe.
export function detailTooltipFormatter(params: unknown): string {
  const items = (Array.isArray(params) ? params : [params]) as TooltipParam[];
  const lines: string[] = [];
  const head = items[0]?.axisValueLabel ?? items[0]?.name;
  if (head) {
    lines.push(escapeHTML(String(head)));
  }
  const seen = new Set<string>();
  for (const p of items) {
    if (p?.seriesName) {
      const v = Array.isArray(p.value) ? p.value[p.value.length - 1] : p.value;
      lines.push(`${p.marker ?? ""}${escapeHTML(p.seriesName)}: ${escapeHTML(formatValue(v))}`);
    }
    const detail = p?.data?.detail;
    if (!Array.isArray(detail)) {
      continue;
    }
    for (const row of detail) {
      const kv = row as { label?: unknown; value?: unknown };
      const line = `${escapeHTML(String(kv?.label ?? ""))}: ${escapeHTML(String(kv?.value ?? ""))}`;
      // Several series share one record's detail (the engine mirrors it), so dedupe across series.
      if (!seen.has(line)) {
        seen.add(line);
        lines.push(line);
      }
    }
  }
  return lines.join("<br/>");
}

function formatValue(v: unknown): string {
  if (v === null || v === undefined) {
    return "-";
  }
  return String(v);
}

function escapeHTML(s: string): string {
  return s.replace(
    /[&<>"']/g,
    (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c] ?? c,
  );
}

// useVisible defers work until the element scrolls near the viewport (a 200px head start), so a page
// with many charts initializes only the visible ones — off-screen charts cost nothing until reached.
// Without IntersectionObserver (older engines, jsdom) everything counts as visible.
function useVisible(ref: React.RefObject<HTMLDivElement | null>) {
  const [visible, setVisible] = useState(false);
  useEffect(() => {
    const el = ref.current;
    if (!el) {
      return;
    }
    if (typeof IntersectionObserver === "undefined") {
      setVisible(true);
      return;
    }
    const io = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting)) {
          setVisible(true);
          io.disconnect();
        }
      },
      { rootMargin: "200px" },
    );
    io.observe(el);
    return () => io.disconnect();
  }, [ref]);
  return visible;
}

interface EChartsFenceProps {
  text: string;
}

// EChartsFence renders a fenced ```echarts block: a pre-resolved ECharts option the static export
// emits in place of ```viewspec fences. A body that is not a JSON object falls back to a plain code
// block, so a malformed block never hides its source.
export function EChartsFence({ text }: EChartsFenceProps) {
  const option = parseOption(text);
  if (option === null) {
    return <CodeBlock lang="json" text={text} />;
  }
  return <EChartsBlock option={option} />;
}

// parseOption accepts only a JSON object (an ECharts option's shape); anything else is null.
export function parseOption(text: string): Record<string, unknown> | null {
  try {
    const parsed: unknown = JSON.parse(text);
    if (parsed !== null && typeof parsed === "object" && !Array.isArray(parsed)) {
      return parsed as Record<string, unknown>;
    }
  } catch {
    // fall through
  }
  return null;
}

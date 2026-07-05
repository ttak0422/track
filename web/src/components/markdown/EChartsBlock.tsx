import { useNavigate } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { CodeBlock } from "./CodeBlock";
import { applyChartTheme, chartThemeFromCSS } from "./echartsTheme";
import { useThemeVersion } from "./MermaidDiagram";

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
      const themed = applyChartTheme(option, chartThemeFromCSS());
      attachDetailTooltip(themed);
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
      if (typeof ResizeObserver !== "undefined") {
        observer = new ResizeObserver(() => chart?.resize());
        observer.observe(el);
      }
    });
    return () => {
      disposed = true;
      observer?.disconnect();
    };
  }, [option, visible, themeVersion, navigate]);

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
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const onWheel = (event: WheelEvent) => {
      if (!event.shiftKey) event.stopPropagation();
    };
    el.addEventListener("wheel", onWheel, { capture: true });
    return () => el.removeEventListener("wheel", onWheel, { capture: true });
  }, []);

  return <div ref={containerRef} className="viewspec-chart" role="img" aria-label="Chart" />;
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

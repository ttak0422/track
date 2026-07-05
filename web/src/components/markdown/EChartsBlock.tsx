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
      chart.setOption(applyChartTheme(option, chartThemeFromCSS()), { notMerge: true });
      if (typeof ResizeObserver !== "undefined") {
        observer = new ResizeObserver(() => chart?.resize());
        observer.observe(el);
      }
    });
    return () => {
      disposed = true;
      observer?.disconnect();
    };
  }, [option, visible, themeVersion]);

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

import { useEffect, useRef } from "react";
import { useViewSpecQuery } from "../../queries";
import { STATIC_MODE } from "../../runtime";
import { CodeBlock } from "./CodeBlock";

interface ViewSpecChartProps {
  text: string;
}

// echartsModule caches the lazy import so every chart on a page shares one load. ECharts is pulled in
// on demand (a separate chunk): pages without charts never download it, and the static export — which
// replaces these blocks with pre-rendered images at build time — never triggers the import at all.
let echartsModule: Promise<typeof import("echarts")> | undefined;
function loadECharts() {
  echartsModule ??= import("echarts");
  return echartsModule;
}

// ViewSpecChart renders fenced ```viewspec blocks (View Spec JSON, see docs/spec/visualization.md) as
// interactive charts. Chart semantics stay decided by the Go server: the block is posted to
// /api/viewspec and the returned ECharts option is handed to a local ECharts instance, so the frontend
// never re-implements chart resolution. The fetch lives in react-query (useViewSpecQuery), so a chart
// re-renders live both when the note edit changes the block text (new query key) and when the vault's
// data/ directory changes (useLiveEvents invalidates the viewspec queries on the server's `data`
// event). A bad spec shows the server's message plus the source at the block position, mirroring
// MermaidDiagram's error state. The static export replaces these blocks with pre-rendered images at
// build time, so in static mode a leftover block (e.g. inside a quoted example) just shows its source.
export function ViewSpecChart({ text }: ViewSpecChartProps) {
  const query = useViewSpecQuery(text);
  const option = query.data?.echarts;
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const el = containerRef.current;
    if (!el || option === undefined) {
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
      chart.setOption(option, { notMerge: true });
      if (typeof ResizeObserver !== "undefined") {
        observer = new ResizeObserver(() => chart?.resize());
        observer.observe(el);
      }
    });
    return () => {
      disposed = true;
      observer?.disconnect();
    };
  }, [option]);

  // Dispose the ECharts instance only on unmount; the option effect above reuses it across updates.
  useEffect(() => {
    const el = containerRef.current;
    return () => {
      if (el) {
        void loadECharts().then((echarts) => echarts.getInstanceByDom(el)?.dispose());
      }
    };
  }, []);

  if (STATIC_MODE) {
    return <CodeBlock lang="json" text={text} />;
  }

  if (query.isError) {
    const message = query.error instanceof Error ? query.error.message : String(query.error);
    return (
      <div className="viewspec-chart viewspec-chart-error">
        <p>View Spec error: {message}</p>
        <CodeBlock lang="json" text={text} />
      </div>
    );
  }

  if (option === undefined) {
    return <div className="viewspec-chart viewspec-chart-loading">Rendering chart...</div>;
  }

  return <div ref={containerRef} className="viewspec-chart" role="img" aria-label="Chart" />;
}

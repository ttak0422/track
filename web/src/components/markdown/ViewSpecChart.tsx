import { useViewSpecQuery } from "../../queries";
import { STATIC_MODE } from "../../runtime";
import { CodeBlock } from "./CodeBlock";
import { EChartsBlock } from "./EChartsBlock";

interface ViewSpecChartProps {
  text: string;
}

// ViewSpecChart renders fenced ```viewspec blocks (View Spec JSON, see docs/spec/visualization.md) as
// interactive charts. Chart semantics stay decided by the Go server: the block is posted to
// /api/viewspec and the returned ECharts option is drawn by EChartsBlock, so the frontend never
// re-implements chart resolution. The fetch lives in react-query (useViewSpecQuery), so a chart
// re-renders live both when the note edit changes the block text (new query key) and when the vault's
// data/ directory changes (useLiveEvents invalidates the viewspec queries on the server's `data`
// event). A bad spec shows the server's message plus the source at the block position, mirroring
// MermaidDiagram's error state. The static export resolves these blocks to ```echarts fences at build
// time, so in static mode a leftover block (e.g. inside a quoted example) just shows its source.
export function ViewSpecChart({ text }: ViewSpecChartProps) {
  const query = useViewSpecQuery(text);

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

  if (query.data === undefined) {
    return <div className="viewspec-chart viewspec-chart-loading">Rendering chart...</div>;
  }

  return <EChartsBlock option={query.data.echarts} />;
}

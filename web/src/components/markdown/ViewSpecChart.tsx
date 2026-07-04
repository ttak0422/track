import { useViewSpecQuery } from "../../queries";
import { STATIC_MODE } from "../../runtime";
import { CodeBlock } from "./CodeBlock";

interface ViewSpecChartProps {
  text: string;
}

// ViewSpecChart renders fenced ```viewspec blocks (View Spec JSON, see docs/spec/visualization.md) as
// charts. Unlike Mermaid, the drawing engine is the Go server: the block is posted to /api/viewspec and
// the returned static SVG is inlined, so the frontend never re-implements chart rendering. The fetch
// lives in react-query (useViewSpecQuery), so a chart re-renders live both when the note edit changes
// the block text (new query key) and when the vault's data/ directory changes (useLiveEvents invalidates
// the viewspec queries on the server's `data` event). A bad spec shows the server's message plus the
// source at the block position, mirroring MermaidDiagram's error state. The static export replaces
// these blocks with pre-rendered images at build time, so in static mode a leftover block (e.g. inside
// a quoted example) just shows its source.
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

  return (
    <div
      className="viewspec-chart"
      role="img"
      aria-label="Chart"
      dangerouslySetInnerHTML={{ __html: query.data.svg }}
    />
  );
}

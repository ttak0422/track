import { useEffect, useState } from "react";
import { renderViewSpec } from "../../api";
import { STATIC_MODE } from "../../runtime";
import { CodeBlock } from "./CodeBlock";

interface ViewSpecChartProps {
  text: string;
}

type ChartState =
  | { status: "loading" }
  | { status: "ready"; svg: string }
  | { status: "error"; message: string };

// ViewSpecChart renders fenced ```viewspec blocks (View Spec JSON, see docs/spec/visualization.md) as
// charts. Unlike Mermaid, the drawing engine is the Go server: the block is posted to /api/viewspec and
// the returned static SVG is inlined, so the frontend never re-implements chart rendering. A bad spec
// shows the server's message plus the source at the block position, mirroring MermaidDiagram's error
// state. The static export replaces these blocks with pre-rendered images at build time, so in static
// mode a leftover block (e.g. inside a quoted example) just shows its source.
export function ViewSpecChart({ text }: ViewSpecChartProps) {
  const [state, setState] = useState<ChartState>({ status: "loading" });

  useEffect(() => {
    if (STATIC_MODE) return;
    let cancelled = false;
    setState({ status: "loading" });
    renderViewSpec(text)
      .then(({ svg }) => {
        // Drop the XML prolog: it is valid in a standalone .svg file but not inside an HTML element.
        if (!cancelled) setState({ status: "ready", svg: svg.replace(/^\s*<\?xml[^>]*>\s*/, "") });
      })
      .catch((error: unknown) => {
        if (!cancelled) setState({ status: "error", message: error instanceof Error ? error.message : String(error) });
      });
    return () => {
      cancelled = true;
    };
  }, [text]);

  if (STATIC_MODE) {
    return <CodeBlock lang="json" text={text} />;
  }

  if (state.status === "error") {
    return (
      <div className="viewspec-chart viewspec-chart-error">
        <p>View Spec error: {state.message}</p>
        <CodeBlock lang="json" text={text} />
      </div>
    );
  }

  if (state.status === "loading") {
    return <div className="viewspec-chart viewspec-chart-loading">Rendering chart...</div>;
  }

  return (
    <div
      className="viewspec-chart"
      role="img"
      aria-label="Chart"
      dangerouslySetInnerHTML={{ __html: state.svg }}
    />
  );
}

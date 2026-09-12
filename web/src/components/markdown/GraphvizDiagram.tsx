import { useEffect, useState } from "react";
import { useVisible } from "../../hooks/useVisible";
import { DiagramFrame, type DiagramState } from "./MermaidDiagram";

interface GraphvizDiagramProps {
  text: string;
}

// GraphvizDiagram renders fenced ```dot blocks with Graphviz compiled to WebAssembly
// (@hpcc-js/wasm-graphviz). It is wired exactly like Mermaid: the engine is imported lazily so a note
// without a diagram never loads it, and a syntax error falls back to the message plus the source. The
// SVG's own colors are fixed (Graphviz has no theming); dark mode is handled by a CSS filter scoped to
// .graphviz-diagram, so no re-render on theme change is needed. Off-screen blocks wait for the
// viewport before importing the WASM engine.
export function GraphvizDiagram({ text }: GraphvizDiagramProps) {
  const { ref, visible } = useVisible<HTMLDivElement>();
  const [state, setState] = useState<DiagramState>({ status: "loading" });

  useEffect(() => {
    if (!visible) return;
    let cancelled = false;
    setState({ status: "loading" });

    async function renderDiagram() {
      try {
        const { Graphviz } = await import("@hpcc-js/wasm-graphviz");
        const graphviz = await Graphviz.load();
        const svg = graphviz.dot(withTransparentBackground(text));
        // Graphviz prefixes an XML prolog and DOCTYPE; keep only the <svg> element for innerHTML.
        const start = svg.indexOf("<svg");
        if (!cancelled) setState({ status: "ready", svg: start >= 0 ? svg.slice(start) : svg });
      } catch (error) {
        if (!cancelled) setState({ status: "error", message: errorMessage(error) });
      }
    }

    void renderDiagram();
    return () => {
      cancelled = true;
    };
  }, [text, visible]);

  return (
    <div ref={ref}>
      <DiagramFrame
        state={state}
        source={text}
        sourceLang="dot"
        label="Graphviz diagram"
        className="graphviz-diagram"
      />
    </div>
  );
}

// withTransparentBackground defaults the graph background to transparent so the diagram sits on the
// panel color in both themes (Graphviz would otherwise paint an opaque white canvas). Injected right
// after the opening brace, so an explicit bgcolor written later in the source still wins.
export function withTransparentBackground(dot: string): string {
  const brace = dot.indexOf("{");
  if (brace < 0) return dot;
  return `${dot.slice(0, brace + 1)} bgcolor="transparent"; ${dot.slice(brace + 1)}`;
}

function errorMessage(error: unknown): string {
  if (error instanceof Error && error.message.trim() !== "") {
    return `Graphviz render failed: ${error.message}`;
  }
  return "Graphviz render failed.";
}

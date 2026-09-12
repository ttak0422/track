import { lazy, Suspense, useEffect, useState } from "react";
import { useVisible } from "../hooks/useVisible";
import type { GraphCanvasProps } from "./GraphCanvas";

// GraphCanvas is the only importer of d3-force (the force-layout engine). Load it on demand so d3-force
// and the canvas code stay out of the initial JS bundle, and first paint is not blocked by them. The
// graph is decorative/secondary in every place it appears (per-note side panel, full graph route), so
// the fallback is empty — the canvas fills in a frame later without shifting the primary content.
// Consumers import GraphCanvas from here instead of ./GraphCanvas.
const GraphCanvasInner = lazy(() =>
  import("./GraphCanvas").then((m) => ({ default: m.GraphCanvas })),
);

export function GraphCanvas(props: GraphCanvasProps) {
  // Render nothing until mounted on the client. renderToString does not support a lazy/Suspense boundary
  // cleanly, so a prerendered page would emit a Suspense fallback that mismatches on hydration; gating on
  // mount makes the server and the first client render agree (both empty), then the canvas loads. The
  // graph is secondary content, so deferring it to after hydration costs nothing above the fold.
  // Off-screen graphs additionally wait for the viewport before importing d3-force, so a help top page
  // that never scrolls to the graph never downloads it.
  const [mounted, setMounted] = useState(false);
  const { ref, visible } = useVisible<HTMLDivElement>();
  useEffect(() => setMounted(true), []);
  if (!mounted || !visible)
    return <div ref={ref} className="graph-canvas" aria-hidden="true" />;
  return (
    <div ref={ref}>
      <Suspense fallback={null}>
        <GraphCanvasInner {...props} />
      </Suspense>
    </div>
  );
}

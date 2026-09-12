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
  const ready = mounted && visible;
  // One host element in both states, so the IntersectionObserver target never swaps mid-load and the
  // layout never shifts when the canvas arrives. The host is the flex item its parent sizes
  // (.aside-graph, .graph-full, .graph-panel, .graph-lightbox all lay a .graph-canvas out as their
  // filling child); the canvas fills the host. A bare wrapper div here would sit between that flex
  // parent and the canvas, the canvas's flex sizing would stop applying, and it would collapse.
  return (
    <div ref={ref} className="graph-canvas-host" aria-hidden={!ready || undefined}>
      {ready ? (
        <Suspense fallback={null}>
          <GraphCanvasInner {...props} />
        </Suspense>
      ) : null}
    </div>
  );
}

import { lazy, Suspense, useEffect, useState } from "react";
import type { GraphCanvasProps } from "./GraphCanvas";

// GraphCanvas is the only importer of d3-force (the force-layout engine). Load it on demand so d3-force
// and the canvas code stay out of the initial JS bundle, and first paint is not blocked by them. The
// graph is decorative/secondary in every place it appears (home background, per-note side panel, full
// graph route), so the fallback is empty — the canvas fills in a frame later without shifting the primary
// content. Consumers import GraphCanvas from here instead of ./GraphCanvas.
const GraphCanvasInner = lazy(() =>
  import("./GraphCanvas").then((m) => ({ default: m.GraphCanvas })),
);

export function GraphCanvas(props: GraphCanvasProps) {
  // Render nothing until mounted on the client. renderToString does not support a lazy/Suspense boundary
  // cleanly, so a prerendered page would emit a Suspense fallback that mismatches on hydration; gating on
  // mount makes the server and the first client render agree (both empty), then the canvas loads. The
  // graph is secondary content, so deferring it to after hydration costs nothing above the fold.
  const [mounted, setMounted] = useState(false);
  useEffect(() => setMounted(true), []);
  if (!mounted) return null;
  return (
    <Suspense fallback={null}>
      <GraphCanvasInner {...props} />
    </Suspense>
  );
}

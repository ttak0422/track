import { lazy, Suspense } from "react";
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
  return (
    <Suspense fallback={null}>
      <GraphCanvasInner {...props} />
    </Suspense>
  );
}

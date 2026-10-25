import type { DrawioGraph, DrawioViewerInstance } from "./drawioViewer";

// GraphViewer's automatic fit resizes its container after fitting, and its responsive option is
// disabled on Safari. Keep the renderer at source scale and fit its complete bounds using SVG's
// own viewport instead. Percentage width follows parent layout changes in every supported engine.
export function fitDrawioViewport(host: HTMLElement, viewer: DrawioViewerInstance): () => void {
  let graph: DrawioGraph | undefined;
  let disposed = false;
  const fit = () => {
    if (!graph) return;
    const svg = host.querySelector("svg");
    if (!svg) return;
    const bounds = graph.getGraphBounds();
    if (![bounds.x, bounds.y, bounds.width, bounds.height].every(Number.isFinite)) return;
    // getGraphBounds includes strokes, labels and edges. Leave a small antialiasing guard too.
    const guard = 2;
    const width = Math.max(1, bounds.width) + guard * 2;
    const height = Math.max(1, bounds.height) + guard * 2;
    svg.setAttribute("viewBox", `${bounds.x - guard} ${bounds.y - guard} ${width} ${height}`);
    svg.setAttribute("preserveAspectRatio", "xMinYMin meet");
    svg.setAttribute("width", String(width));
    svg.setAttribute("height", String(height));
    Object.assign(svg.style, {
      display: "block", width: "100%", height: "auto", maxWidth: `${width}px`,
      minWidth: "0", minHeight: "0",
    });
    Object.assign(host.style, { width: "100%", height: "auto", maxWidth: "100%", minWidth: "0" });
  };
  const initialized = () => {
    if (!viewer.graph || graph === viewer.graph) return;
    graph?.removeListener(fit);
    graph = viewer.graph;
    graph.resizeContainer = false;
    graph.addListener("size", fit);
    fit();
    // The vendor's resize sensor calls graph.fit even when automatic fit is disabled. SVG now
    // handles resizing itself. Deferred initialization installs the sensor after its event fires,
    // so release it at the end of this initialization turn as well as during disposal.
    queueMicrotask(() => { if (!disposed) window.ResizeSensor?.detach(host); });
  };
  // A hidden host defers graph construction until it becomes measurable.
  viewer.addListener("graphInitialized", initialized);
  initialized();
  return () => {
    disposed = true;
    viewer.removeListener(initialized);
    graph?.removeListener(fit);
    window.ResizeSensor?.detach(host);
    graph?.destroy();
  };
}

import type { GraphNode } from "../types";

// radiusForNode returns the radius a graph node is drawn at, in CSS pixels independent of zoom: the
// centre keeps its focal size, and the rest take the engine's precomputed five-level grade (1–5) —
// the same grade in every view, so a node's size does not depend on which graph shows it. A node
// without a grade (an older bundle, an orphan report) falls back to the degree-based size.
// Rendering and hit-testing both use it, so the clickable area always matches the drawn dot.
// Exported for its test.
export function radiusForNode(node: GraphNode & { degree?: number }, centerID: string): number {
  const center = node.center || node.note_id === centerID;
  if (center) return 10;
  if (typeof node.size === "number" && node.size >= 1 && node.size <= 5) {
    return 4.5 + node.size * 1.5;
  }
  return 6 + Math.min(8, Math.sqrt(node.degree ?? 0) * 2);
}

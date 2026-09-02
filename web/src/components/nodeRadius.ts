import type { GraphNode } from "../types";

// radiusForNode returns the radius a graph node is drawn at, in CSS pixels independent of zoom: the
// centre keeps its focal size, and the rest take the engine's precomputed five-level grade (1–5) —
// the same grade in every view, so a node's size does not depend on which graph shows it. A node
// without a grade (an older bundle, an orphan report) falls back to the degree-based size.
// Rendering and hit-testing both use it, so the clickable area always matches the drawn dot.
// Exported for its test.
export function radiusForNode(node: GraphNode & { degree?: number }, centerID: string): number {
  const center = node.center || node.note_id === centerID;
  const graded =
    typeof node.size === "number" && node.size >= 1 && node.size <= 5
      ? GRADE_RADIUS[node.size - 1]
      : undefined;
  // The centre keeps its focal size, but never draws smaller than its own grade would: a hub read as
  // a stub the moment you opened it.
  if (center) return Math.max(10, graded ?? 0);
  return graded ?? 6 + Math.min(8, Math.sqrt(node.degree ?? 0) * 2);
}

// The five grades as radii, one per level rather than a ramp: the 1.5px step the ramp gave between
// neighbouring grades was a difference you had to go looking for, which is no difference at all in a
// field of dots. Each level is about 1.4× the last instead, so the gap between a stub and a hub is a
// factor of four across the radius — visible without comparing two nodes side by side.
const GRADE_RADIUS = [4, 6, 8.5, 12, 17];

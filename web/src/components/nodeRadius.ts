import type { GraphNode } from "../types";

// The engine supplies a continuous area score for the allowed population in [1, 5]. Interpolate area,
// then take its square root so the weight applies to area rather than being squared again.
// Rendering, label placement and hit-testing share this CSS-pixel radius, independent of zoom.
export function radiusForNode(node: GraphNode & { degree?: number }, centerID: string): number {
  const center = node.center || node.note_id === centerID;
  const scaled =
    typeof node.area_score === "number" && node.area_score >= 1 && node.area_score <= 5
      ? Math.sqrt(4 ** 2 + ((node.area_score - 1) / 4) * (17 ** 2 - 4 ** 2))
      : undefined;
  const graded =
    typeof node.size === "number" && Number.isInteger(node.size) && node.size >= 1 && node.size <= 5
      ? GRADE_RADIUS[node.size - 1]
      : undefined;
  const radius = scaled ?? graded ?? 6 + Math.min(8, Math.sqrt(node.degree ?? 0) * 2);
  // The centre retains its focal minimum; its color also distinguishes it from its neighbours.
  if (center) return Math.max(10, scaled ?? graded ?? 0);
  return radius;
}

const GRADE_RADIUS = [4, 6, 8.5, 12, 17];

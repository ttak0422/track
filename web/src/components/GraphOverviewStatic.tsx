import { useMemo } from "react";
import { useThemeVersion } from "../hooks/useThemeVersion";
import type { Graph, GraphNode, NoteID } from "../types";
import { radiusForNode } from "./nodeRadius";

export interface GraphOverviewStaticProps {
  graph: Graph;
  onSelect: (noteID: NoteID) => void;
}

// Padding around the layout's bounding box, in the same world units the server's coordinates use.
const VIEWBOX_PADDING = 48;

function cssColor(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

// GraphOverviewStatic draws the whole-vault link overview as a static SVG picture. The server has
// already laid the nodes out (GraphNode.x/y), so there is no simulation here and no per-tick redraw:
// one <line> per edge, one <circle> per node, fitted to the layout's bounding box — an overview of
// how notes connect, not a map to navigate. Nodes carry their title as a native tooltip (no drawn
// labels) and clicking one opens the note.
export function GraphOverviewStatic({ graph, onSelect }: GraphOverviewStaticProps) {
  // Colors are read from the tokens once per theme change; the hook bumps on a light/dark switch,
  // which re-renders with the newly resolved values.
  const themeVersion = useThemeVersion();
  const colors = useMemo(
    () => ({
      edge: cssColor("--line-strong"),
      nodeFill: cssColor("--bg"),
      nodeStroke: cssColor("--line-node"),
    }),
    [themeVersion],
  );

  const placed = useMemo(
    () => (graph.nodes || []).filter((n) => typeof n.x === "number" && typeof n.y === "number"),
    [graph],
  );
  const byID = useMemo(() => new Map(placed.map((n) => [n.note_id, n])), [placed]);
  const edges = useMemo(
    () =>
      (graph.edges || [])
        .map((edge) => {
          const source = byID.get(edge.source_id);
          const target = byID.get(edge.target_id);
          return source && target ? { source, target } : undefined;
        })
        .filter((e) => e !== undefined),
    [byID, graph.edges],
  );

  const viewBox = useMemo(() => {
    if (placed.length === 0) return null;
    let minX = Infinity;
    let minY = Infinity;
    let maxX = -Infinity;
    let maxY = -Infinity;
    for (const n of placed) {
      minX = Math.min(minX, n.x as number);
      minY = Math.min(minY, n.y as number);
      maxX = Math.max(maxX, n.x as number);
      maxY = Math.max(maxY, n.y as number);
    }
    return {
      x: minX - VIEWBOX_PADDING,
      y: minY - VIEWBOX_PADDING,
      w: Math.max(1, maxX - minX) + VIEWBOX_PADDING * 2,
      h: Math.max(1, maxY - minY) + VIEWBOX_PADDING * 2,
    };
  }, [placed]);

  if (!viewBox) return null;

  // Node radii reuse the aside graph's five-level grade (the engine precomputed it per note), so a
  // hub reads as a hub in every view at whatever scale the fit lands on.
  const radiusFor = (node: GraphNode) => radiusForNode(node, String(graph.center_id));

  return (
    <svg
      className="graph-overview"
      aria-label="Graph"
      role="img"
      viewBox={`${viewBox.x} ${viewBox.y} ${viewBox.w} ${viewBox.h}`}
      preserveAspectRatio="xMidYMid meet"
    >
      {edges.map((edge) => (
        <line
          key={`e-${edge.source.note_id}-${edge.target.note_id}`}
          x1={edge.source.x}
          y1={edge.source.y}
          x2={edge.target.x}
          y2={edge.target.y}
          stroke={colors.edge}
          strokeWidth={1}
        />
      ))}
      {placed.map((node) => (
        <circle
          key={node.note_id}
          cx={node.x}
          cy={node.y}
          r={radiusFor(node)}
          fill={colors.nodeFill}
          stroke={colors.nodeStroke}
          strokeWidth={1}
          onClick={() => onSelect(node.note_id)}
        >
          <title>{node.title || `#${node.note_id}`}</title>
        </circle>
      ))}
    </svg>
  );
}

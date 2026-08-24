import Graph from "graphology";
import type { Attributes } from "graphology-types";
import type { Graph as TrackGraph } from "../types";
import { radiusForNode } from "./nodeRadius";

// Node attributes the WebGL overview draws with. size mirrors the canvas graph's five-level grade
// radii (nodeRadius.ts) so a node keeps its size in every view, and x/y are the same deterministic
// ring seed d3-force started from — the ForceAtlas2 layout begins there, so the same vault opens
// into the same picture instead of a fresh scatter each visit.
export interface OverviewNodeAttributes extends Attributes {
  label: string;
  size: number;
  center: boolean;
  x: number;
  y: number;
}

export interface OverviewEdgeAttributes extends Attributes {}

// graphToGraphology converts a track /api/graph payload into the graphology instance sigma renders.
// Edges whose endpoints the payload never delivered are dropped (the canvas graph did the same), and
// self/duplicate edges are collapsed because sigma's default graph is simple and undirected. Kept
// free of DOM/sigma so it can be tested under jsdom, where WebGL cannot run.
export function graphToGraphology(
  graph: TrackGraph,
): Graph<OverviewNodeAttributes, OverviewEdgeAttributes> {
  const rendered = new Graph<OverviewNodeAttributes, OverviewEdgeAttributes>({
    multi: false,
    type: "undirected",
  });

  const nodes = graph.nodes || [];
  const edges = graph.edges || [];

  // Incident-edge counts feed radiusForNode's size fallback for nodes without a precomputed grade.
  const degrees = new Map<string, number>();
  for (const edge of edges) {
    degrees.set(edge.source_id, (degrees.get(edge.source_id) ?? 0) + 1);
    degrees.set(edge.target_id, (degrees.get(edge.target_id) ?? 0) + 1);
  }

  const centerID = String(graph.center_id ?? "");
  // A single node sits at the origin rather than on a ring of one; otherwise node i takes angle
  // 2πi/n on a 160-radius circle — the seed ring GraphCanvas lays d3-force out from.
  const isolated = nodes.length === 1;
  const order = Math.max(1, nodes.length);

  nodes.forEach((node, index) => {
    const id = String(node.note_id);
    const angle = (Math.PI * 2 * index) / order;
    rendered.addNode(id, {
      label: node.title || `#${id}`,
      size: radiusForNode({ ...node, degree: degrees.get(id) ?? 0 }, centerID),
      center: Boolean(node.center || node.note_id === graph.center_id),
      x: isolated ? 0 : Math.cos(angle) * 160,
      y: isolated ? 0 : Math.sin(angle) * 160,
    });
  });

  for (const edge of edges) {
    if (!rendered.hasNode(edge.source_id) || !rendered.hasNode(edge.target_id)) continue;
    if (edge.source_id === edge.target_id || rendered.hasEdge(edge.source_id, edge.target_id)) {
      continue;
    }
    rendered.addEdge(edge.source_id, edge.target_id);
  }

  return rendered;
}

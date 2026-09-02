import type { Graph, GraphEdge, NoteID } from "../types";

// How many nodes the whole-vault overview will draw. The bound comes from what a screen can show,
// not from what the layout can compute: a 1920x1080 canvas holds about 2,300 dots at a 30px pitch
// with no room left for the edges between them, so a node-link picture stops saying anything well
// before the renderer stops keeping up. Making the renderer faster buys nothing past this point —
// what has to scale is the choice of what to draw. Past the cap the view names what it left out.
export const OVERVIEW_NODE_CAP = 1000;

export interface OverviewGraph {
  graph: Graph;
  // Notes the overview did not draw, so the view can say so rather than quietly showing a slice.
  hidden: number;
}

// overviewGraph reduces the whole-vault graph to the part worth drawing. A note with no link is not
// in the link graph, so it is not in a picture of the link graph — the vault's unlinked notes have
// their own listing (track graph --orphans), where they read as a list far better than as a field of
// dots. Beyond the cap the best-connected notes are kept: cutting by degree keeps the structure an
// overview is for, where cutting by recency would leave a random slice hanging off edges to notes
// that are not there.
export function overviewGraph(graph: Graph, cap = OVERVIEW_NODE_CAP): OverviewGraph {
  const nodes = graph.nodes || [];
  const known = new Set(nodes.map((node) => node.note_id));
  // A self link and a link to a note the payload never delivered draw nothing, and counting them
  // would let a node survive the filter on a link that is not on screen.
  let edges = (graph.edges || []).filter(
    (edge) =>
      edge.source_id !== edge.target_id && known.has(edge.source_id) && known.has(edge.target_id),
  );

  const degree = degreeOf(edges);
  let kept = nodes.filter((node) => degree.has(node.note_id));
  if (kept.length > cap) {
    kept = [...kept].sort(
      (a, b) =>
        (degree.get(b.note_id) ?? 0) - (degree.get(a.note_id) ?? 0) || compareID(a.note_id, b.note_id),
    );
    kept = kept.slice(0, cap);
    const inSlice = new Set(kept.map((node) => node.note_id));
    edges = edges.filter((edge) => inSlice.has(edge.source_id) && inSlice.has(edge.target_id));
    // The cut can strand a node whose only neighbours fell outside it, and an overview that draws
    // nothing unconnected must not start now. ponytail: one pass — dropping a stranded node can
    // strand its own neighbour in turn, which this does not chase; iterate to a fixed point only if
    // a fringe of loose dots ever shows up.
    const linked = degreeOf(edges);
    kept = kept.filter((node) => linked.has(node.note_id));
  }

  return { graph: { ...graph, nodes: kept, edges }, hidden: nodes.length - kept.length };
}

// degreeOf counts incident edges per node. Only nodes with at least one edge appear, so membership
// alone answers "is this note in the link graph".
function degreeOf(edges: GraphEdge[]): Map<NoteID, number> {
  const degree = new Map<NoteID, number>();
  for (const edge of edges) {
    degree.set(edge.source_id, (degree.get(edge.source_id) ?? 0) + 1);
    degree.set(edge.target_id, (degree.get(edge.target_id) ?? 0) + 1);
  }
  return degree;
}

// Ties break on note id so the same vault always yields the same slice.
function compareID(a: NoteID, b: NoteID): number {
  return a < b ? -1 : a > b ? 1 : 0;
}

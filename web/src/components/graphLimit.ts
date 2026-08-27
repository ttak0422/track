// The whole-vault graph (GraphPanel, GraphFullView) refuses to render past this many nodes: past it
// the force layout still converges, but the canvas turns into an undifferentiated blob that reads as
// noise. The note-aside local graph is bounded by construction, so the limit only applies to the
// whole-vault surfaces.
export const MAX_GRAPH_NODES = 500;

// graphCountCaption is the small label shown beside the whole-vault graph, and reused inside the
// overflow message's parenthetical, so the two always agree on what the vault holds.
export function graphCountCaption(nodeCount: number): string {
  return `${nodeCount} ${nodeCount === 1 ? "note" : "notes"}`;
}

// graphTooManyMessage is the English message shown in place of the canvas once a vault passes the
// node limit. The count is the actual node count, not a rounded figure.
export function graphTooManyMessage(nodeCount: number): string {
  return `Too many notes to display (${graphCountCaption(nodeCount)}).`;
}
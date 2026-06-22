import { useMemo } from "react";
import { useDebouncedValue } from "../hooks/useDebouncedValue";
import { useGraphQuery } from "../queries";
import { useSearchState } from "../searchState";
import type { Graph, NoteID } from "../types";
import { GraphCanvas } from "./GraphCanvas";

// Renders the whole-vault graph as a non-interactive background decoration. While the home search box
// has a query, the matching notes light up (debounced) and the rest dim in place — the constellation
// keeps its shape instead of dropping nodes, so typing highlights rather than hides.
export function GraphBackground() {
  const globalGraph = useGraphQuery(true);
  const { query } = useSearchState();
  const debouncedQuery = useDebouncedValue(query, 180);
  const graph = globalGraph.data?.graph;

  const highlightIds = useMemo(() => matchingIds(graph, debouncedQuery), [graph, debouncedQuery]);

  if (!graph) {
    return null;
  }

  return (
    <div className="graph-background" aria-hidden="true">
      <GraphCanvas
        graph={graph}
        resetToken={0}
        onSelect={() => {}}
        decorative
        highlightIds={highlightIds}
      />
    </div>
  );
}

// matchingIds returns the ids of nodes whose title matches the query (case-insensitive), or null for an
// empty query so the canvas draws every node normally instead of dimming the unmatched ones.
function matchingIds(graph: Graph | undefined, query: string): ReadonlySet<NoteID> | null {
  const term = query.trim().toLowerCase();
  if (!graph || term === "") {
    return null;
  }
  const ids = new Set<NoteID>();
  for (const node of graph.nodes) {
    if (node.title.toLowerCase().includes(term)) {
      ids.add(node.note_id);
    }
  }
  return ids;
}

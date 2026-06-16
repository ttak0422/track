import { useMemo } from "react";
import { useDebouncedValue } from "../hooks/useDebouncedValue";
import { useGraphQuery } from "../queries";
import { useSearchState } from "../searchState";
import type { Graph } from "../types";
import { GraphCanvas } from "./GraphCanvas";

// Renders the whole-vault graph as a non-interactive background decoration. While the home search box
// has a query, the graph narrows to the matching notes in real time (debounced) so typing visibly
// filters the constellation. GraphCanvas re-fits whenever the graph it receives changes, and edges to
// dropped nodes are pruned for us, so filtering the node list here is enough.
export function GraphBackground() {
  const globalGraph = useGraphQuery(true);
  const { query } = useSearchState();
  const debouncedQuery = useDebouncedValue(query, 180);
  const graph = globalGraph.data?.graph;

  const filtered = useMemo(() => filterGraph(graph, debouncedQuery), [graph, debouncedQuery]);

  if (!filtered) {
    return null;
  }

  return (
    <div className="graph-background" aria-hidden="true">
      <GraphCanvas graph={filtered} resetToken={0} onSelect={() => {}} decorative />
    </div>
  );
}

// filterGraph keeps only nodes whose title matches the query (case-insensitive). An empty query returns
// the original graph unchanged so the full constellation shows. Returns the same reference when there is
// nothing to filter, keeping GraphCanvas from re-seeding on every render.
function filterGraph(graph: Graph | undefined, query: string): Graph | undefined {
  if (!graph) {
    return undefined;
  }
  const term = query.trim().toLowerCase();
  if (term === "") {
    return graph;
  }
  const nodes = graph.nodes.filter((node) => node.title.toLowerCase().includes(term));
  return { ...graph, nodes };
}

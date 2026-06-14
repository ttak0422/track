import { useGraphQuery } from "../queries";
import { GraphCanvas } from "./GraphCanvas";

// Renders the whole-vault graph as a non-interactive background decoration.
export function GraphBackground() {
  const globalGraph = useGraphQuery(true);
  const graph = globalGraph.data?.graph;

  if (!graph) {
    return null;
  }

  return (
    <div className="graph-background" aria-hidden="true">
      <GraphCanvas graph={graph} resetToken={0} onSelect={() => {}} decorative />
    </div>
  );
}

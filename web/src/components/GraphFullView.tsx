import { useNavigate } from "@tanstack/react-router";
import { useGraphQuery } from "../queries";
import { GraphOverviewSigma } from "./GraphOverviewSigma";

// GraphFullView draws the whole vault graph filling the reader, in an ordinary "Graph" tab. The
// rendering lives in GraphOverviewSigma (sigma.js/WebGL): the previous d3-force canvas simulated
// every node inline and repainted the whole canvas each tick, which stopped being usable at this
// vault's scale. The reduced view carries click-to-navigate, pan/zoom, and a reset; the hover
// previews went with that renderer.
export function GraphFullView() {
  const graphQuery = useGraphQuery(true);
  const navigate = useNavigate();
  const graph = graphQuery.data?.graph;

  return (
    <div className="graph-full" aria-label="Graph">
      {graphQuery.isPending ? <p className="muted graph-message">Loading graph...</p> : null}
      {graphQuery.isError ? <p className="error graph-message">{graphQuery.error.message}</p> : null}
      {graph ? (
        <GraphOverviewSigma
          graph={graph}
          onSelect={(noteID) => void navigate({ to: "/notes/$noteId", params: { noteId: String(noteID) } })}
        />
      ) : null}
    </div>
  );
}

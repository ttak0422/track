import { useNavigate } from "@tanstack/react-router";
import { useGraphQuery } from "../queries";
import { GraphOverviewStatic } from "./GraphOverviewStatic";

// GraphFullView draws the whole vault's link overview filling the reader. The server lays the nodes
// out and this view only renders the picture (GraphOverviewStatic): an overview of how notes
// connect, fitted to its own bounding box — no pan, zoom or reset chrome, and no hover previews.
// Clicking a node opens the note, like any other view.
export function GraphFullView() {
  const graphQuery = useGraphQuery(true);
  const navigate = useNavigate();
  const graph = graphQuery.data?.graph;

  return (
    <div className="graph-full" aria-label="Graph">
      {graphQuery.isPending ? <p className="muted graph-message">Loading graph...</p> : null}
      {graphQuery.isError ? <p className="error graph-message">{graphQuery.error.message}</p> : null}
      {graph ? (
        <GraphOverviewStatic
          graph={graph}
          onSelect={(noteID) => void navigate({ to: "/notes/$noteId", params: { noteId: String(noteID) } })}
        />
      ) : null}
    </div>
  );
}

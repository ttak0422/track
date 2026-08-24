import { useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useGraphQuery } from "../queries";
import type { NoteID } from "../types";
import { IconRotate2, RailIcon } from "./icons";
import { GraphOverviewCanvas } from "./GraphOverviewCanvas";

// GraphFullView draws the whole vault's graph filling the reader as a connection overview. The
// layout is computed once on the client (graphStaticLayout) and drawn in a single pass — the old
// force simulation ran every node for hundreds of ticks, which at vault scale meant a blank canvas
// for seconds and, once drawn, a hairball no easier to read than rings around clusters. Hovering
// points at a node and clicking navigates to it; per-node previews live on the note pages, which
// name their own neighbourhoods far better than floating windows over three thousand dots could.
// It lives in an ordinary "Graph" tab, so it carries only the canvas and a bottom-right reset
// control.
export function GraphFullView() {
  const graphQuery = useGraphQuery(true);
  const navigate = useNavigate();
  const [resetToken, setResetToken] = useState(0);
  const graph = graphQuery.data?.graph;

  return (
    <div className="graph-full" aria-label="Graph">
      {graphQuery.isPending ? <p className="muted graph-message">Loading graph...</p> : null}
      {graphQuery.isError ? <p className="error graph-message">{graphQuery.error.message}</p> : null}
      {graph ? (
        <GraphOverviewCanvas
          graph={graph}
          resetToken={resetToken}
          onSelect={(noteID: NoteID) =>
            void navigate({ to: "/notes/$noteId", params: { noteId: String(noteID) } })
          }
        />
      ) : null}
      <div className="graph-controls">
        <button
          className="graph-reset"
          type="button"
          aria-label="Reset graph view"
          title="Reset graph view"
          onClick={() => setResetToken((token) => token + 1)}
        >
          <RailIcon Icon={IconRotate2} size={15} />
        </button>
      </div>
    </div>
  );
}

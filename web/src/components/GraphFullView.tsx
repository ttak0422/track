import { useState } from "react";
import { useGraphQuery } from "../queries";
import type { NoteID } from "../types";
import { GraphCanvas } from "./GraphCanvas";
import { initialPreviewBounds, type PreviewAnchor } from "./preview/bounds";
import { useFloating } from "./preview/floatingStore";

interface Point {
  x: number;
  y: number;
}

// GraphFullView draws the whole graph filling the reader. It lives in an ordinary "Graph" tab (closed
// from the tab strip, no chrome of its own), so it carries only the canvas and a bottom-right reset
// control mirroring the corner graph panel. Pressing a node opens that note in a floating window, so you
// can explore the graph and pop notes open without leaving it.
export function GraphFullView() {
  const graphQuery = useGraphQuery(true);
  const floating = useFloating();
  const [resetToken, setResetToken] = useState(0);
  const graph = graphQuery.data?.graph;

  function openNote(noteID: NoteID, point: Point) {
    // Opens like a normal note popup: unpinned, so pin toggles persistence and × closes.
    floating.open({ kind: "note", noteID }, initialPreviewBounds(graphPointAnchor(point)), false, false);
  }

  return (
    <div className="graph-full" aria-label="Graph">
      {graphQuery.isPending ? <p className="muted graph-message">Loading graph...</p> : null}
      {graphQuery.isError ? <p className="error graph-message">{graphQuery.error.message}</p> : null}
      {graph ? <GraphCanvas graph={graph} resetToken={resetToken} onSelect={openNote} /> : null}
      <div className="graph-controls">
        <button
          className="graph-reset"
          type="button"
          aria-label="Reset graph view"
          title="Reset graph view"
          onClick={() => setResetToken((token) => token + 1)}
        >
          ↺
        </button>
      </div>
    </div>
  );
}

export function graphPointAnchor(point: Point): PreviewAnchor {
  const x = point.x;
  const y = point.y;
  return { linkLeft: x, linkRight: x, linkTop: y, linkBottom: y };
}

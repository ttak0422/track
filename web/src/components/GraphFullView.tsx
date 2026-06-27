import { useState } from "react";
import { useGraphQuery } from "../queries";
import type { NoteID } from "../types";
import { GraphCanvas } from "./GraphCanvas";
import { initialPreviewBounds, type PreviewAnchor } from "./preview/bounds";
import { useFloating } from "./preview/floatingStore";

// GraphFullView draws the whole graph filling the screen. Unlike the corner graph panel (which
// navigates on click), pressing a node here opens that note in a pinned floating window, so you can
// explore the graph and pop notes open without leaving it.
export function GraphFullView({ onClose }: { onClose: () => void }) {
  const graphQuery = useGraphQuery(true);
  const floating = useFloating();
  const [resetToken, setResetToken] = useState(0);
  const graph = graphQuery.data?.graph;

  function openNote(noteID: NoteID) {
    // Opens like a normal note popup: unpinned, so pin toggles persistence and × closes.
    floating.open({ kind: "note", noteID }, initialPreviewBounds(centerAnchor()), false, false);
  }

  return (
    <div className="graph-full" role="dialog" aria-label="Graph">
      <div className="graph-full-bar">
        <span className="graph-full-title">Graph</span>
        <button
          className="graph-reset"
          type="button"
          aria-label="Reset graph view"
          title="Reset graph view"
          onClick={() => setResetToken((token) => token + 1)}
        >
          ↺
        </button>
        <button className="graph-full-close" type="button" aria-label="Close graph" title="Close" onClick={onClose}>
          ×
        </button>
      </div>
      {graphQuery.isPending ? <p className="muted graph-message">Loading graph...</p> : null}
      {graphQuery.isError ? <p className="error graph-message">{graphQuery.error.message}</p> : null}
      {graph ? <GraphCanvas graph={graph} resetToken={resetToken} onSelect={openNote} /> : null}
    </div>
  );
}

// A point near the upper-left third of the viewport, so the opened note window lands beside it with room.
function centerAnchor(): PreviewAnchor {
  const x = window.innerWidth * 0.4;
  const y = window.innerHeight * 0.3;
  return { linkLeft: x, linkRight: x, linkTop: y, linkBottom: y };
}

import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useGraphQuery } from "../queries";
import type { NoteID } from "../types";
import { GraphCanvas } from "./GraphCanvasLazy";
import { type PreviewAnchor, initialPreviewBounds } from "./preview/bounds";
import { useFloating } from "./preview/floatingStore";
import { pointerCanHover, previewOpenDelay } from "./preview/stack";
import { IconRotate2, RailIcon } from "./icons";
import { overviewGraph } from "./overviewGraph";

interface Point {
  x: number;
  y: number;
}

// GraphFullView draws the whole graph filling the reader. Nodes follow the same popup model as wiki
// links on a note page: hovering a node shows a transient preview, dragging it makes the preview stick,
// pinning promotes it to the floating layer, and clicking the node navigates to it. It lives in an
// ordinary "Graph" tab, so it carries only the canvas and a bottom-right reset control.
export function GraphFullView() {
  const graphQuery = useGraphQuery(true);
  const navigate = useNavigate();
  const floating = useFloating();
  const [resetToken, setResetToken] = useState(0);
  // The whole vault is more than a picture can hold, so the view draws the link graph's connected
  // part up to a cap and says what it left out (overviewGraph).
  const overview = useMemo(
    () => (graphQuery.data?.graph ? overviewGraph(graphQuery.data.graph) : null),
    [graphQuery.data?.graph],
  );
  const graph = overview?.graph;

  // Hovering a node opens a transient preview in the floating layer — the same window a wiki link
  // opens, in the same flat stack, so it can be raised over one opened earlier and outlives whatever
  // opened it. All this view keeps is the intent timer and a handle on what it opened last.
  const openTimer = useRef<number | undefined>(undefined);
  const pendingRef = useRef<{ noteID: NoteID; anchor: PreviewAnchor } | null>(null);
  const openedRef = useRef<string | null>(null);

  useEffect(() => () => cancelOpen(), []);

  function cancelOpen() {
    if (openTimer.current !== undefined) {
      window.clearTimeout(openTimer.current);
      openTimer.current = undefined;
    }
    pendingRef.current = null;
  }

  function holdPreview() {
    if (openedRef.current) floating.hold(openedRef.current);
  }

  function scheduleClose() {
    // Leaving before the intent delay cancels a pending open, so a node the pointer only passed over
    // never pops once the cursor has moved on.
    cancelOpen();
    if (openedRef.current) floating.scheduleClose(openedRef.current);
  }

  // Drives the preview from the canvas: a node id rests it open (after the intent delay), null lets it
  // close.
  function onHover(noteID: NoteID | null, point: Point) {
    if (noteID === null) {
      scheduleClose();
      return;
    }
    // A touch drag across the canvas reports hovers the whole way; on a pointer that cannot hover the
    // node is opened by the tap that navigates to it, not by a window (see pointerCanHover).
    if (!pointerCanHover()) return;
    holdPreview();
    // Already showing this node: hold it where it is rather than chasing the cursor. Asked of the
    // layer, not of a local flag, so a window the reader closed can be hovered open again.
    const shown = floating.windows.find((win) => win.id === openedRef.current);
    if (shown && shown.content.kind === "note" && shown.content.noteID === noteID) return;
    pendingRef.current = { noteID, anchor: graphPointAnchor(point) };
    if (openTimer.current !== undefined) return;
    openTimer.current = window.setTimeout(() => {
      openTimer.current = undefined;
      const pending = pendingRef.current;
      if (!pending) return;
      openedRef.current = floating.open(
        { kind: "note", noteID: pending.noteID },
        initialPreviewBounds(pending.anchor),
        false,
        { transient: true, anchor: pending.anchor },
      );
    }, previewOpenDelay);
  }

  return (
    <div className="graph-full" aria-label="Graph">
      {graphQuery.isPending ? <p className="muted graph-message">Loading graph...</p> : null}
      {graphQuery.isError ? <p className="error graph-message">{graphQuery.error.message}</p> : null}
      {graph ? (
        <GraphCanvas
          graph={graph}
          resetToken={resetToken}
          onHover={onHover}
          onSelect={(noteID) => void navigate({ to: "/notes/$noteId", params: { noteId: String(noteID) } })}
        />
      ) : null}
      {overview && overview.hidden > 0 ? (
        <p className="graph-scope">{overview.hidden.toLocaleString()} notes not drawn</p>
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

export function graphPointAnchor(point: Point): PreviewAnchor {
  const x = point.x;
  const y = point.y;
  return { linkLeft: x, linkRight: x, linkTop: y, linkBottom: y };
}

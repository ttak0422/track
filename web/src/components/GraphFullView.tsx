import { useEffect, useRef, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useGraphQuery } from "../queries";
import type { NoteID } from "../types";
import { GraphCanvas } from "./GraphCanvas";
import { type PreviewAnchor, type PreviewBounds, initialPreviewBounds } from "./preview/bounds";
import { useFloating } from "./preview/floatingStore";
import { NoteWindow } from "./preview/NoteWindow";
import { nextPreviewStackOrder, previewOpenDelay } from "./preview/stack";

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
  const graph = graphQuery.data?.graph;

  // A single transient hover preview. Dragging it (sticky) keeps it until closed; pinning promotes it to
  // the floating layer, which is what holds multiple persistent windows. ponytail: this mirrors
  // WikiLink's hover-intent machine; unify into a shared hook if a third consumer appears.
  const [preview, setPreview] = useState<{ noteID: NoteID; anchor: PreviewAnchor } | null>(null);
  const [sticky, setSticky] = useState(false);
  const [stackOrder, setStackOrder] = useState(nextPreviewStackOrder);
  const openTimer = useRef<number | undefined>(undefined);
  const closeTimer = useRef<number | undefined>(undefined);
  const pendingRef = useRef<{ noteID: NoteID; anchor: PreviewAnchor } | null>(null);

  useEffect(
    () => () => {
      if (openTimer.current !== undefined) window.clearTimeout(openTimer.current);
      if (closeTimer.current !== undefined) window.clearTimeout(closeTimer.current);
    },
    [],
  );

  function holdPreview() {
    if (closeTimer.current !== undefined) {
      window.clearTimeout(closeTimer.current);
      closeTimer.current = undefined;
    }
  }

  function cancelOpen() {
    if (openTimer.current !== undefined) {
      window.clearTimeout(openTimer.current);
      openTimer.current = undefined;
    }
    pendingRef.current = null;
  }

  function scheduleClose() {
    // Leaving before the intent delay cancels a pending open, so a node the pointer only passed over
    // never pops once the cursor has moved on.
    cancelOpen();
    if (sticky || closeTimer.current !== undefined) return;
    closeTimer.current = window.setTimeout(() => {
      closeTimer.current = undefined;
      setPreview(null);
    }, 220);
  }

  // Drives the preview from the canvas: a node id rests it open (after the intent delay), null lets it
  // close. A sticky preview is left alone so a new hover does not steal a window the user kept.
  function onHover(noteID: NoteID | null, point: Point) {
    if (noteID === null) {
      scheduleClose();
      return;
    }
    holdPreview();
    if (sticky) return;
    if (preview?.noteID === noteID) return; // already showing this node; don't chase the cursor
    pendingRef.current = { noteID, anchor: graphPointAnchor(point) };
    if (openTimer.current !== undefined) return;
    openTimer.current = window.setTimeout(() => {
      openTimer.current = undefined;
      if (pendingRef.current) {
        setStackOrder(nextPreviewStackOrder());
        setPreview(pendingRef.current);
      }
    }, previewOpenDelay);
  }

  function bringPreviewToFront() {
    setStackOrder(nextPreviewStackOrder());
  }

  function detachPreview() {
    holdPreview();
    setSticky(true);
  }

  // Pinning promotes the transient preview into the persistent floating layer at its current bounds.
  function promote(bounds: PreviewBounds, collapsed: boolean) {
    if (!preview) return;
    floating.open({ kind: "note", noteID: preview.noteID }, bounds, collapsed, true);
    setSticky(false);
    setPreview(null);
  }

  function closePreview() {
    setSticky(false);
    setPreview(null);
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
      {preview ? (
        <NoteWindow
          noteID={preview.noteID}
          initialBounds={initialPreviewBounds(preview.anchor)}
          reanchor={sticky ? undefined : preview.anchor}
          pinned={false}
          depth={0}
          stackOrder={stackOrder}
          onActivate={bringPreviewToFront}
          onHold={holdPreview}
          onLeave={scheduleClose}
          onDetach={detachPreview}
          onClose={closePreview}
          onPinToggle={promote}
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

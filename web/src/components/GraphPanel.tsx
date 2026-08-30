import { useNavigate } from "@tanstack/react-router";
import { PointerEvent, useMemo, useRef, useState } from "react";
import { useGraphQuery } from "../queries";
import { GraphCanvas } from "./GraphCanvasLazy";
import { IconAffiliate, IconRotate2, IconX, RailIcon } from "./icons";
import { overviewGraph } from "./overviewGraph";

// The floating whole-vault graph, behind a corner launcher. It only mounts on views without a graph
// of their own (day, tags, search, the empty state): note pages carry an always-on local graph in
// their aside, and the full-graph and calendar routes have their own surfaces.

interface PanelSize {
  width: number;
  height: number;
}

interface ResizeState {
  pointerId: number;
  startX: number;
  startY: number;
  startWidth: number;
  startHeight: number;
  moved: boolean;
}

const defaultWidth = 520;
const defaultHeight = 380;
const minWidth = 280;
const minHeight = 220;

export function GraphPanel() {
  const [resetToken, setResetToken] = useState(0);
  const [visible, setVisible] = useState(false);
  const [panelSize, setPanelSize] = useState<PanelSize>(() => ({
    width: Math.min(defaultWidth, window.innerWidth - 36),
    height: Math.min(defaultHeight, window.innerHeight - 112),
  }));
  const resizeRef = useRef<ResizeState | null>(null);
  // The whole-vault graph is not cheap; fetch it only once the panel is opened.
  const state = useGraphQuery(visible);
  const navigate = useNavigate();

  // Same whole-vault graph as the Graph tab, so it takes the same bound (see overviewGraph).
  const overview = useMemo(
    () => (state.data?.graph ? overviewGraph(state.data.graph) : null),
    [state.data?.graph],
  );
  const graph = overview?.graph;

  function onHandleDown(event: PointerEvent<HTMLButtonElement>) {
    event.preventDefault();
    event.currentTarget.setPointerCapture(event.pointerId);
    resizeRef.current = {
      pointerId: event.pointerId,
      startX: event.clientX,
      startY: event.clientY,
      startWidth: panelSize.width,
      startHeight: panelSize.height,
      moved: false,
    };
  }

  function onHandleMove(event: PointerEvent<HTMLButtonElement>) {
    const drag = resizeRef.current;
    if (!drag || drag.pointerId !== event.pointerId) return;
    // The panel is anchored bottom-right, so dragging the top-left handle up
    // and to the left grows the panel.
    const dx = drag.startX - event.clientX;
    const dy = drag.startY - event.clientY;
    if (Math.abs(dx) + Math.abs(dy) > 4) {
      drag.moved = true;
    }
    setPanelSize({
      width: clamp(drag.startWidth + dx, minWidth, window.innerWidth - 36),
      height: clamp(drag.startHeight + dy, minHeight, window.innerHeight - 112),
    });
  }

  function onHandleUp(event: PointerEvent<HTMLButtonElement>) {
    const drag = resizeRef.current;
    resizeRef.current = null;
    if (!drag || drag.pointerId !== event.pointerId) return;
    event.currentTarget.releasePointerCapture(event.pointerId);
  }

  if (!visible) {
    return (
      <button
        className="graph-fab"
        type="button"
        aria-label="Show graph"
        title="Show graph"
        onClick={() => setVisible(true)}
      >
        <GraphGlyph />
      </button>
    );
  }

  return (
    <aside
      className="graph-panel"
      aria-label="Graph"
      style={{ width: panelSize.width, height: panelSize.height }}
    >
      <button
        className="graph-resize-handle"
        type="button"
        aria-label="Resize graph (drag)"
        title="Drag to resize"
        onPointerDown={onHandleDown}
        onPointerMove={onHandleMove}
        onPointerUp={onHandleUp}
      />
      {state.isPending ? <p className="muted graph-message">Loading graph...</p> : null}
      {state.isError ? <p className="error graph-message">{state.error.message}</p> : null}
      {graph ? (
        <GraphCanvas
          graph={graph}
          resetToken={resetToken}
          onSelect={(noteID) =>
            void navigate({ to: "/notes/$noteId", params: { noteId: String(noteID) } })
          }
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
        <button
          className="graph-reset"
          type="button"
          aria-label="Hide graph"
          title="Hide graph"
          onClick={() => setVisible(false)}
        >
          <RailIcon Icon={IconX} size={15} />
        </button>
      </div>
    </aside>
  );
}

function clamp(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(Math.max(min, max), value));
}

function GraphGlyph() {
  return <RailIcon Icon={IconAffiliate} size={22} />;
}

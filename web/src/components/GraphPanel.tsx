import { useNavigate, useRouterState } from "@tanstack/react-router";
import { PointerEvent, useRef, useState } from "react";
import { useGraphQuery, useLocalGraphQuery } from "../queries";
import type { NoteID } from "../types";
import { GraphCanvas } from "./GraphCanvas";

type GraphScope = "local" | "global";

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

const scopes: GraphScope[] = ["local", "global"];
const defaultWidth = 520;
const defaultHeight = 380;
const minWidth = 280;
const minHeight = 220;

export function GraphPanel() {
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const selectedNoteID = noteIDFromPath(pathname);
  const [scope, setScope] = useState<GraphScope>("local");
  const [resetToken, setResetToken] = useState(0);
  const [visible, setVisible] = useState(false);
  const [panelSize, setPanelSize] = useState<PanelSize>(() => ({
    width: Math.min(defaultWidth, window.innerWidth - 36),
    height: Math.min(defaultHeight, window.innerHeight - 112),
  }));
  const resizeRef = useRef<ResizeState | null>(null);
  const effectiveScope = selectedNoteID === undefined ? "global" : scope;
  const localGraph = useLocalGraphQuery(
    selectedNoteID,
    effectiveScope === "local" && selectedNoteID !== undefined,
  );
  const globalGraph = useGraphQuery(effectiveScope === "global");
  const navigate = useNavigate();

  const state = effectiveScope === "local" ? localGraph : globalGraph;
  const graph = state.data?.graph;

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
          focusNodeID={effectiveScope === "global" ? selectedNoteID : undefined}
          onSelect={(noteID) =>
            void navigate({ to: "/notes/$noteId", params: { noteId: String(noteID) } })
          }
        />
      ) : null}
      <div className="graph-controls">
        <div className="graph-scope" role="group" aria-label="Graph scope">
          {scopes.map((option) => (
            <button
              aria-pressed={effectiveScope === option}
              disabled={option === "local" && selectedNoteID === undefined}
              key={option}
              type="button"
              onClick={() => setScope(option)}
            >
              {scopeLabel(option)}
            </button>
          ))}
        </div>
        <button
          className="graph-reset"
          type="button"
          aria-label="Reset graph view"
          title="Reset graph view"
          onClick={() => setResetToken((token) => token + 1)}
        >
          ↺
        </button>
        <button
          className="graph-reset"
          type="button"
          aria-label="Hide graph"
          title="Hide graph"
          onClick={() => setVisible(false)}
        >
          ×
        </button>
      </div>
    </aside>
  );
}

function noteIDFromPath(pathname: string): NoteID | undefined {
  const match = /^\/notes\/(\d+)$/.exec(pathname);
  if (!match) return undefined;
  const noteID = Number(match[1]);
  return Number.isSafeInteger(noteID) && noteID > 0 ? noteID : undefined;
}

function scopeLabel(scope: GraphScope): string {
  return scope[0].toUpperCase() + scope.slice(1);
}

function clamp(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(Math.max(min, max), value));
}

function GraphGlyph() {
  return (
    <svg viewBox="0 0 24 24" width="22" height="22" aria-hidden="true">
      <line x1="6" y1="7" x2="17" y2="6" stroke="currentColor" strokeWidth="1.5" />
      <line x1="6" y1="7" x2="12" y2="17" stroke="currentColor" strokeWidth="1.5" />
      <line x1="17" y1="6" x2="12" y2="17" stroke="currentColor" strokeWidth="1.5" />
      <circle cx="6" cy="7" r="2.6" fill="currentColor" />
      <circle cx="17" cy="6" r="2.6" fill="currentColor" />
      <circle cx="12" cy="17" r="2.6" fill="currentColor" />
    </svg>
  );
}

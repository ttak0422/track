import { useNavigate, useRouterState } from "@tanstack/react-router";
import { useState } from "react";
import { useGraphQuery, useLocalGraphQuery } from "../queries";
import type { NoteID } from "../types";
import { GraphCanvas } from "./GraphCanvas";

type GraphScope = "local" | "global";

const scopes: GraphScope[] = ["local", "global"];

export function GraphPanel() {
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const selectedNoteID = noteIDFromPath(pathname);
  const [scope, setScope] = useState<GraphScope>("local");
  const [resetToken, setResetToken] = useState(0);
  const effectiveScope = selectedNoteID === undefined ? "global" : scope;
  const localGraph = useLocalGraphQuery(
    selectedNoteID,
    effectiveScope === "local" && selectedNoteID !== undefined,
  );
  const globalGraph = useGraphQuery(effectiveScope === "global");
  const navigate = useNavigate();

  const state = effectiveScope === "local" ? localGraph : globalGraph;
  const graph = state.data?.graph;

  return (
    <aside className="graph-panel" aria-label="Graph">
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

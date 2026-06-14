import { useNavigate, useRouterState } from "@tanstack/react-router";
import { useMemo, useState } from "react";
import { useGraphQuery, useLocalGraphQuery } from "../queries";
import type { Graph, NoteID } from "../types";
import { GraphCanvas } from "./GraphCanvas";

type GraphScope = "local" | "global";

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
  const meta = useMemo(() => graphMeta(graph), [graph]);

  return (
    <aside className="graph-panel" aria-label="Graph">
      <header className="graph-header">
        <div>
          <h2>{effectiveScope === "local" ? "Local Graph" : "Global Graph"}</h2>
          <p>{meta}</p>
        </div>
        <button
          className="secondary-button"
          type="button"
          onClick={() => setScope((current) => (current === "local" ? "global" : "local"))}
        >
          {effectiveScope === "local" ? "Global" : "Local"}
        </button>
      </header>
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
      <button
        className="graph-reset"
        type="button"
        aria-label="Reset graph view"
        title="Reset graph view"
        onClick={() => setResetToken((token) => token + 1)}
      >
        Reset
      </button>
    </aside>
  );
}

function noteIDFromPath(pathname: string): NoteID | undefined {
  const match = /^\/notes\/(\d+)$/.exec(pathname);
  if (!match) return undefined;
  const noteID = Number(match[1]);
  return Number.isSafeInteger(noteID) && noteID > 0 ? noteID : undefined;
}

function graphMeta(graph: Graph | undefined): string {
  if (!graph) return "";
  return `${graph.nodes.length} notes, ${graph.edges.length} links`;
}

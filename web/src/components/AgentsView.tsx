import { Link } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { useAgentLog, useAgents } from "../queries";
import { STATIC_MODE } from "../runtime";
import type { AgentBlock, AgentLog, AgentMessage, AgentSession, AgentStatus } from "../types";
import { MarkdownView } from "./MarkdownView";

export function AgentsView() {
  const agentsQuery = useAgents();
  const sessions = agentsQuery.data?.sessions ?? [];
  const [selectedID, setSelectedID] = useState("");

  useEffect(() => {
    if (selectedID === "" && sessions[0]) setSelectedID(sessions[0].sessionId);
    if (selectedID !== "" && !sessions.some((session) => session.sessionId === selectedID)) {
      setSelectedID(sessions[0]?.sessionId ?? "");
    }
  }, [selectedID, sessions]);

  const selected = sessions.find((session) => session.sessionId === selectedID);
  const logQuery = useAgentLog(selectedID, selectedID !== "");

  if (STATIC_MODE) {
    return <div className="agents-view"><p className="muted">Agent sessions are not published.</p></div>;
  }

  return (
    <div className="agents-view" aria-label="Agent sessions">
      <header className="day-head">
        <h1 className="day-title">Agents</h1>
        {sessions.length > 0 ? <p className="muted">{sessions.length} active</p> : null}
      </header>
      {agentsQuery.isPending ? <p className="muted">Loading...</p> : null}
      {agentsQuery.isError ? <p className="error">{agentsQuery.error.message}</p> : null}
      {agentsQuery.data && sessions.length === 0 ? <p className="muted">No agent sessions.</p> : null}
      <div className="agents-layout">
        <div className="agents-list backlink-list day-list" aria-label="Sessions">
          {sessions.map((session) => (
            <SessionRow
              key={session.sessionId}
              session={session}
              selected={session.sessionId === selectedID}
              onSelect={() => setSelectedID(session.sessionId)}
            />
          ))}
        </div>
        <section className="agent-detail" aria-label="Transcript">
          {selected ? (
            <>
              <div className="agent-detail-head">
                <div>
                  <p className="agent-section-label">Session</p>
                  <h2>{logQuery.data?.aiTitle ?? selected.name}</h2>
                </div>
                {logQuery.data?.pr ? (
                  <a href={logQuery.data.pr.url} target="_blank" rel="noreferrer">
                    PR #{logQuery.data.pr.number}
                  </a>
                ) : null}
              </div>
              {logQuery.isPending ? <p className="muted">Loading transcript...</p> : null}
              {logQuery.isError ? <p className="error">{logQuery.error.message}</p> : null}
              {logQuery.data ? <MarkdownView markdown={transcriptMarkdown(logQuery.data)} showTitle={false} /> : null}
            </>
          ) : <p className="muted">Select a session to read its transcript.</p>}
        </section>
      </div>
    </div>
  );
}

function SessionRow({
  session,
  selected,
  onSelect,
}: {
  session: AgentSession;
  selected: boolean;
  onSelect: () => void;
}) {
  return (
    <div
      className={`agent-row${selected ? " is-selected" : ""}`}
      role="button"
      tabIndex={0}
      onClick={onSelect}
      onKeyDown={(event) => {
        if (event.key === "Enter" || event.key === " ") onSelect();
      }}
    >
      <span className={`note-state-badge agent-state-${session.status}`} title={session.waitingFor}>
        {statusLabel(session.status)}
      </span>
      <strong>{session.name}</strong>
      {session.branch ? <span className="agent-branch">{session.branch}</span> : null}
      <span className="agent-cwd">{session.cwd}</span>
      {session.note ? (
        <Link
          className="agent-note"
          to="/notes/$noteId"
          params={{ noteId: session.note.note_id }}
          onClick={(event) => event.stopPropagation()}
        >
          {session.note.title}
        </Link>
      ) : null}
      <time className="agent-updated" dateTime={new Date(session.updatedAt).toISOString()}>
        {relativeTime(session.updatedAt)}
      </time>
      {session.waitingFor ? <span className="agent-waiting-for">{session.waitingFor}</span> : null}
    </div>
  );
}

function statusLabel(status: AgentStatus): string {
  return status === "waiting" ? "要対応" : status === "busy" ? "稼働" : "待機";
}

function relativeTime(value: number): string {
  const seconds = Math.max(0, Math.floor((Date.now() - value) / 1000));
  if (seconds < 60) return "たった今";
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}分前`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}時間前`;
  return `${Math.floor(hours / 24)}日前`;
}

export function transcriptMarkdown(log: AgentLog): string {
  return log.messages.map(messageMarkdown).join("\n\n");
}

function messageMarkdown(message: AgentMessage): string {
  // Variant 6, section label: speaker changes are hierarchy by label and space, not decoration.
  const speaker = message.type === "assistant" ? "ASSISTANT" : "USER";
  const body = message.message.map(blockMarkdown).filter(Boolean).join("\n\n");
  return `### ${speaker}\n\n${body}`;
}

function blockMarkdown(block: AgentBlock): string {
  if (block.type === "text") return block.text ?? "";
  if (block.type === "tool_use") return `*tool: ${block.name ?? "unknown"}*`;
  if (block.type === "tool_result") return "*tool result*";
  if (block.type === "thinking") return "*thinking*";
  return `*${block.type}*`;
}

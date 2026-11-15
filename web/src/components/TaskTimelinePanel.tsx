import { useId, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { getTaskTimeline } from "../api";
import type { NoteID, TaskTimeline } from "../types";

// Read on demand: merely visiting a note does not fetch its transition history.
// The panel is mounted only by the live editor, never the published-site reader.
export function TaskTimelinePanel({ noteID }: { noteID: NoteID }) {
  const [open, setOpen] = useState(false);
  const id = useId();
  return (
    <section className="task-timeline" aria-label="Task timeline">
      <button className="task-timeline-toggle" type="button" aria-expanded={open}
        aria-controls={id} onClick={() => setOpen(!open)}>
        Task timeline
      </button>
      <div id={id}>{open ? <TimelineRead noteID={noteID} /> : null}</div>
    </section>
  );
}

function TimelineRead({ noteID }: { noteID: NoteID }) {
  const query = useQuery({
    // Task writes and live change events invalidate this shared prefix.
    queryKey: ["tasks", "timeline", noteID],
    queryFn: ({ signal }) => getTaskTimeline(noteID, signal),
    refetchInterval: 30_000,
  });
  return <>
    {query.isPending ? <p className="muted" role="status">Loading timeline...</p> : null}
    {query.isError ? <p className="error" role="alert">{query.error.message}</p> : null}
    {query.isError && query.data ? <p className="muted">Showing the last retrieved timeline; it may be stale.</p> : null}
    {query.data ? <TimelineSnapshotView timeline={query.data} /> : null}
  </>;
}

export function TimelineSnapshotView({ timeline }: { timeline: TaskTimeline }) {
  const { progress, items, history } = timeline;
  return <>
    <p className="muted">Saved tasks: {progress.done} done, {progress.cancelled} cancelled,
      {" "}{progress.open} open ({progress.total} total).</p>
    {items.length === 0 ? <p className="muted">No tasks in this note.</p> : (
      <ol className="task-timeline-items" aria-label="Current tasks">
        {items.map((item) => <li key={item.line} style={{ paddingInlineStart: `${item.depth}em` }}>
          <span className="muted">{item.state}</span> {item.text}
          <small className="muted"> (line {item.line})</small>
        </li>)}
      </ol>
    )}
    <h3>Recorded transitions</h3>
    <p className="muted">In recorded order. Line numbers refer to the note at the time of each change,
      not necessarily its current tasks. Direct file edits are not recorded.</p>
    {history.length === 0 ? <p className="muted">No recorded transitions.</p> : (
      <ol className="task-timeline-history" aria-label="Recorded transitions">
        {history.map((entry, index) => <li key={index}>
          <time dateTime={entry.at}>{entry.at}</time>{" "}
          <span>{entry.from} → {entry.to}</span>{" "}
          {entry.text ? <span>{entry.text}</span> : null}
          <small className="muted"> (historical line {entry.line})</small>
        </li>)}
      </ol>
    )}
  </>;
}

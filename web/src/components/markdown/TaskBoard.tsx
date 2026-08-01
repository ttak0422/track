import { useContext, useState } from "react";
import { TaskBoardContext } from "./context";
import { useSetTaskStateMutation } from "../../queries";
import { STATIC_MODE } from "../../runtime";
import { taskStates } from "../../taskStates";
import type { TaskItem } from "../../types";

// TaskBoard renders a ```taskboard fence as a kanban board of the surrounding note's tasks: one
// column per task state, one card per task line. In the live workspace a card can be dragged
// to another column (or moved with its state select), which calls the engine's state-set API — the
// same write path as `track task set`, so completion stamps, the sidecar log, and progress cookies
// all apply. The published static site renders the same board read-only.
export function TaskBoard() {
  const { noteID, tasks, etag } = useContext(TaskBoardContext);
  const mutation = useSetTaskStateMutation(noteID);
  const [dragOver, setDragOver] = useState<string>("");

  if (!tasks || tasks.items.length === 0) {
    return <p className="muted">No tasks in this note.</p>;
  }

  const editable = !STATIC_MODE && noteID !== "" && etag !== undefined;

  function moveTask(line: number, state: string, expect: string) {
    if (!editable || mutation.isPending) return;
    mutation.mutate({ line, state, expect, etag: etag! });
  }

  return (
    <div className="task-board" role="group" aria-label="Task board">
      {mutation.isError ? <p className="error">{mutation.error.message}</p> : null}
      <div className="task-board-columns">
        {taskStates.map((state) => {
          const items = tasks.items.filter((item) => item.state === state.name);
          return (
            <section
              key={state.name}
              className={`task-column${dragOver === state.name ? " task-column-dragover" : ""}`}
              aria-label={`${state.name} column`}
              onDragOver={
                editable
                  ? (event) => {
                      event.preventDefault();
                      setDragOver(state.name);
                    }
                  : undefined
              }
              onDragLeave={editable ? () => setDragOver("") : undefined}
              onDrop={
                editable
                  ? (event) => {
                      event.preventDefault();
                      setDragOver("");
                      const line = Number(event.dataTransfer.getData("text/plain"));
                      // The dragged card's state as this board drew it: the write is refused if
                      // the line has since moved on.
                      const dragged = tasks?.items.find((t) => t.line === line);
                      if (Number.isInteger(line) && line > 0 && dragged) {
                        moveTask(line, state.name, dragged.state);
                      }
                    }
                  : undefined
              }
            >
              <header className="task-column-header">
                <span className="task-column-name">{state.name}</span>
                <span className="task-column-count">{items.length}</span>
              </header>
              {items.map((item) => (
                <TaskCard
                  key={item.line}
                  item={item}
                  editable={editable}
                  onMove={(state) => moveTask(item.line, state, item.state)}
                />
              ))}
            </section>
          );
        })}
      </div>
    </div>
  );
}

function TaskCard({
  item,
  editable,
  onMove,
}: {
  item: TaskItem;
  editable: boolean;
  onMove: (state: string) => void;
}) {
  return (
    <article
      className={`task-card${item.done ? " task-card-done" : ""}`}
      draggable={editable}
      onDragStart={
        editable ? (event) => event.dataTransfer.setData("text/plain", String(item.line)) : undefined
      }
    >
      <div className="task-card-text">{item.text === "" ? "(untitled task)" : item.text}</div>
      <div className="task-card-meta">
        {item.priority ? <span className="task-chip task-chip-priority">#{item.priority}</span> : null}
        {item.scheduled ? <span className="task-chip">▷ {item.scheduled}</span> : null}
        {item.due ? <span className="task-chip task-chip-due">! {item.due}</span> : null}
        {item.completed ? <span className="task-chip">✓ {item.completed}</span> : null}
        {editable ? (
          // The select is the keyboard/touch counterpart of dragging, backed by the same API call.
          <select
            className="task-card-state"
            aria-label="Task state"
            value={item.state}
            onChange={(event) => onMove(event.currentTarget.value)}
          >
            {taskStates.map((state) => (
              <option key={state.name} value={state.name}>
                {state.name}
              </option>
            ))}
          </select>
        ) : null}
      </div>
    </article>
  );
}

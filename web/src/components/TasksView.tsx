import { Link } from "@tanstack/react-router";
import { useOpenTasksQuery } from "../queries";
import { STATIC_MODE } from "../runtime";
import type { TaskRow } from "../types";

// TasksView is the vault's open work in one list: every task not in a terminal state, worst first
// (the engine's priority order — [#A] before [#B] before unprioritized, ties by deadline). Most of a
// project note's checklist carries no date at all, so the calendar's dated listing cannot show it and
// this page is the only place it is visible.
//
// It reads, it does not write. A task's identity is its line number in the file, which is stable only
// against the note on disk, so changing state belongs on the note page where the line is in view.
// Every row navigates there.
export function TasksView() {
  // The published bundle holds only the dated listing, so there is nothing to read here.
  const tasksQuery = useOpenTasksQuery(!STATIC_MODE);

  if (STATIC_MODE) {
    return (
      <div className="tasks-view">
        <p className="muted">Tasks are not published.</p>
      </div>
    );
  }

  const tasks = tasksQuery.data?.tasks ?? [];

  return (
    <div className="tasks-view" aria-label="Open tasks">
      <header className="day-head">
        <h1 className="day-title">Tasks</h1>
        {tasks.length > 0 ? <p className="muted">{tasks.length} open</p> : null}
      </header>
      {tasksQuery.isPending ? <p className="muted">Loading...</p> : null}
      {tasksQuery.isError ? <p className="error">{tasksQuery.error.message}</p> : null}
      {tasksQuery.data ? (
        tasks.length === 0 ? (
          <p className="muted">Nothing to do.</p>
        ) : (
          <div className="backlink-list day-list">
            {tasks.map((task) => (
              <Link
                className="backlink day-task"
                key={`${task.note_id}:${task.line}`}
                to="/notes/$noteId"
                params={{ noteId: String(task.note_id) }}
              >
                <span className="day-task-mark">{taskMark(task)}</span>
                {task.text}
                <span className="day-task-note">{task.title}</span>
              </Link>
            ))}
          </div>
        )
      ) : null}
    </div>
  );
}

// taskMark labels a row with the one thing that decides its place in the order: its priority, or —
// for the unprioritized — whether a deadline is what is driving it. "!" reads as a deadline here as
// it does in the day page and the task table.
export function taskMark(task: Pick<TaskRow, "priority" | "due">): string {
  if (task.priority) return task.priority;
  return task.due ? "!" : "▷";
}

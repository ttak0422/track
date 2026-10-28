import { Link } from "@tanstack/react-router";
import type { TaskRow } from "../types";
import { STATIC_MODE } from "../runtime";
import { agendaTasks } from "./calendarPlanningTasks";

export function CalendarAgenda({ tasks, days, today }: { tasks: TaskRow[]; days: string[]; today: string }) {
  const agenda = agendaTasks(tasks, days, today);
  return (
    <div className="calendar-agenda">
      <p className="muted">Open dated tasks · local dates · weeks start Sunday. Tasks without dates stay in their notes.</p>
      {agenda.overdue.length > 0 ? (
        <section className="day-tasks" aria-label="Overdue tasks">
          <h2>Overdue before {today}</h2>
          <TaskLinks tasks={agenda.overdue} />
        </section>
      ) : null}
      {agenda.days.map(({ day, tasks: dayTasks }) => (
        <section className="day-tasks" aria-label={`Tasks on ${day}`} key={day}>
          <h2>{STATIC_MODE ? day : <Link to="/day/$date" params={{ date: day }}>{day}</Link>}{day === today ? " · Today" : ""}</h2>
          {dayTasks.length ? <TaskLinks tasks={dayTasks} /> : <p className="muted">No open tasks scheduled or due.</p>}
        </section>
      ))}
      <p className="muted">{STATIC_MODE ? "Read-only snapshot. Open a task’s note for details." : "Open a task’s note to change its state or dates."}</p>
    </div>
  );
}

function TaskLinks({ tasks }: { tasks: TaskRow[] }) {
  return (
    <div className="backlink-list day-list">
      {tasks.map((task) => (
        <Link className="backlink day-task" key={`${task.note_id}:${task.line}`} to="/notes/$noteId" params={{ noteId: task.note_id }}>
          <span className="day-task-mark">{task.priority ?? "▷"}</span>
          {task.text}
          <span className="day-task-note">{task.title} · {task.state}</span>
          <span className="calendar-task-dates">
            {task.scheduled ? <span>Scheduled {task.scheduled}</span> : null}
            {task.due ? <span>Due {task.due}</span> : null}
          </span>
        </Link>
      ))}
    </div>
  );
}

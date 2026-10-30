import { Link, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { openJournal } from "../api";
import { useAgendaQuery, useDatedTasksQuery, useResolveQuery } from "../queries";
import { STATIC_MODE } from "../runtime";
import { useVaultScope } from "../vaultScope";
import { shiftDay } from "./activityDates";
import { CalendarEventsPanel } from "./CalendarEventsPanel";
import { CalendarAgendaFilter, type AgendaContent } from "./CalendarAgendaFilter";

// DayView is the page a calendar day opens: the notes active that day (the same set the reader's
// "On this day" aside shows) as a plain list of links. The header offers the day's journal — the live
// workspace opens (creating if needed) it like the activity heatmap does; the static site links it only
// when the journal is published.
export function DayView({ date }: { date: string }) {
  const [content, setContent] = useState<AgendaContent>("all");
  const valid = /^\d{4}-\d{2}-\d{2}$/.test(date);
  const agendaQuery = useAgendaQuery(date, "", { enabled: valid });
  const tasksQuery = useDatedTasksQuery();
  // Journal titles are the day's yyyyMMdd, so resolving that term finds the published journal.
  const journalQuery = useResolveQuery(STATIC_MODE && valid ? date.replaceAll("-", "") : "");
  const navigate = useNavigate();
  const { scope } = useVaultScope();

  if (!valid) {
    return <p className="error">Invalid date: {date}</p>;
  }

  async function openDayJournal() {
    try {
      const { note_id } = await openJournal(date, scope);
      navigate({ to: "/notes/$noteId", params: { noteId: String(note_id) } });
    } catch {
      // A failed open simply leaves the user on the current view.
    }
  }

  const journal = journalQuery.data?.found ? journalQuery.data.note : undefined;
  // Tasks planned for this day, from the same listing the calendar filled — so arriving from a
  // calendar cell paints without a fetch.
  const dayTasks = (tasksQuery.data?.tasks ?? []).filter(
    (task) => task.scheduled === date || task.due === date,
  );

  return (
    <div className="day-view" aria-label={`Notes on ${date}`}>
      <header className="day-head">
        <h1 className="day-title">{date}</h1>
        <div className="calendar-nav">
          <button type="button" onClick={() => navigate({ to: "/day/$date", params: { date: shiftDay(date, -1) } })}>
            Previous day
          </button>
          <button type="button" onClick={() => navigate({ to: "/day/$date", params: { date: shiftDay(date, 1) } })}>
            Next day
          </button>
          {!STATIC_MODE ? (
            <button type="button" title="Open this day's journal" onClick={openDayJournal}>
              Journal
            </button>
          ) : journal ? (
            <Link to="/notes/$noteId" params={{ noteId: String(journal.note_id) }} title="Open this day's journal">
              Journal
            </Link>
          ) : null}
        </div>
      </header>
      <CalendarAgendaFilter value={content} onChange={setContent} />
      <div hidden={content === "tasks"}>
        <CalendarEventsPanel days={[date]} defaultDate={date} timeGrid />
      </div>
      {agendaQuery.isPending ? <p className="muted">Loading...</p> : null}
      {agendaQuery.isError ? <p className="error">{agendaQuery.error.message}</p> : null}
      {agendaQuery.data ? (
        agendaQuery.data.notes.length === 0 ? (
          <p className="muted">No notes were worked on this day.</p>
        ) : (
          <div className="backlink-list day-list">
            {agendaQuery.data.notes.map((note) => (
              <Link
                className="backlink"
                key={note.note_id}
                to="/notes/$noteId"
                params={{ noteId: String(note.note_id) }}
              >
                {note.title}
              </Link>
            ))}
          </div>
        )
      ) : null}
      <div hidden={content === "events"}>
        {tasksQuery.isPending ? <p className="muted" role="status">Loading tasks...</p> : null}
        {tasksQuery.isError ? <p className="error" role="alert">Could not load tasks: {tasksQuery.error.message}</p> : null}
        {tasksQuery.data ? (
          <section className="day-tasks">
            <h2>Tasks</h2>
            {dayTasks.length === 0 ? <p className="muted">No dated tasks on this day.</p> : null}
            <div className="backlink-list day-list">
              {dayTasks.map((task) => (
                <Link
                  className="backlink day-task"
                  key={`${task.note_id}:${task.line}`}
                  to="/notes/$noteId"
                  params={{ noteId: String(task.note_id) }}
                >
                  {/* The marker says which date put the task on this day: due reads as a deadline, so
                      it carries the same "!" the task table's due column uses. */}
                  <span className="day-task-mark">{task.due === date ? "!" : "▷"}</span>
                  {task.text}
                  <span className="day-task-note">{task.title}</span>
                </Link>
              ))}
            </div>
          </section>
        ) : null}
      </div>
    </div>
  );
}

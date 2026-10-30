import { Link } from "@tanstack/react-router";
import { useEffect, useMemo, useState } from "react";
import { useDatedTasksQuery, useNotesQuery } from "../queries";
import type { NoteID, SearchResult, TaskRow } from "../types";
import { dueBar } from "./calendarTasks";
import { CalendarAgenda } from "./CalendarAgenda";
import { CalendarEventsPanel } from "./CalendarEventsPanel";
import { CalendarAgendaFilter, type AgendaContent } from "./CalendarAgendaFilter";
import { useCalendarEventsQuery } from "../calendarEventQueries";
import { eventOnDay } from "../calendarEvents";
import { useVaultScope } from "../vaultScope";
import { calendarWeek } from "./calendarPlanningTasks";
import { calendarDateKey, calendarDayRange, shiftCivilDay } from "../calendarDates";
import { useCalendarPreferences } from "../calendarPreferences";
import { CalendarDisplaySettings } from "./CalendarDisplaySettings";
import { IconChevronLeft, IconChevronRight, RailIcon } from "./icons";

const WEEKDAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
// How many note titles a day cell lists before collapsing the rest into a "+N" count.
const CELL_NOTES = 3;
// How many task rows a cell lists before its own "+N". Tasks take the upper block: what the day owes
// (a deadline) matters more than where its words went (the note titles below).
const CELL_TASKS = 3;

// CalendarFullView fills the reader with a month calendar of note activity. Each day cell lists the top
// notes active that day (from the notes listing's activity days) and links to the /day page, which shows
// the full list. Journals carry no activity days, so cells list only real notes; the month title still
// links to the yyyyMM summary journal when one exists. Everything derives from the notes list, which
// both the live server (/api/notes) and the static export (notes.json) provide, so the view needs no
// endpoint of its own — and no journals — to work.
export function CalendarFullView() {
  const notesQuery = useNotesQuery();
  // Month activity remains usable while tasks load; the agenda reports task loading/errors directly.
  const tasksQuery = useDatedTasksQuery();
  const { scope } = useVaultScope();
  const eventsQuery = useCalendarEventsQuery(scope);
  const { settings, zone } = useCalendarPreferences();
  const [month, setMonth] = useState(() => currentMonth(zone));
  const [view, setView] = useState<"month" | "day" | "week">("month");
  const [content, setContent] = useState<AgendaContent>("all");
  const [, refreshDate] = useState(0);
  const todayKey = calendarDateKey(Date.now(), zone);
  const [offset, setOffset] = useState(0);
  // Relative views follow the display-zone midnight, including when a sleeping window returns.
  useEffect(() => {
    const refresh = () => refreshDate((value) => value + 1);
    const midnight = calendarDayRange(todayKey, zone).end;
    const timer = window.setTimeout(refresh, Math.max(50, midnight - Date.now() + 50));
    window.addEventListener("focus", refresh);
    return () => {
      window.clearTimeout(timer);
      window.removeEventListener("focus", refresh);
    };
  }, [todayKey, zone]);

  // day (YYYY-MM-DD) → notes active that day, kept in the listing's order — most recently updated
  // first, the one order every note-list surface shares — so a cell's visible titles read the same as
  // the day page they open.
  const notesByDay = useMemo(() => {
    const map = new Map<string, SearchResult[]>();
    for (const note of notesQuery.data?.notes ?? []) {
      for (const day of note.days ?? []) {
        const list = map.get(day);
        if (list) list.push(note);
        else map.set(day, [note]);
      }
    }
    return map;
  }, [notesQuery.data]);

  // day → the dated tasks that fall on it. A task with both a scheduled and a due date lands on both
  // days (an agenda has an entry for "start Monday" and one for "due Friday"), but never twice on the
  // same day.
  const tasksByDay = useMemo(() => {
    const map = new Map<string, TaskRow[]>();
    for (const task of tasksQuery.data?.tasks ?? []) {
      for (const day of new Set([task.scheduled, task.due].filter(Boolean) as string[])) {
        const list = map.get(day);
        if (list) list.push(task);
        else map.set(day, [task]);
      }
    }
    return map;
  }, [tasksQuery.data]);

  // Journal notes are date-addressed by title (yyyyMM months), so the title is the lookup key directly.
  const journals = useMemo(() => {
    const map = new Map<string, NoteID>();
    for (const note of notesQuery.data?.notes ?? []) {
      if (note.file_kind === "journal") map.set(note.title, note.note_id);
    }
    return map;
  }, [notesQuery.data]);

  const year = Number(month.slice(0, 4));
  const monthNo = Number(month.slice(5, 7));
  const daysInMonth = new Date(Date.UTC(year, monthNo, 0)).getUTCDate();
  // Align the civil month with the selected week start, independently of display-zone clock changes.
  const leadingBlanks = (new Date(`${month}-01T12:00:00Z`).getUTCDay() - settings.weekStartsOn + 7) % 7;
  const monthLabel = `${year} / ${pad2(monthNo)}`;
  const monthNoteID = journals.get(`${year}${pad2(monthNo)}`);

  function shiftMonth(delta: number) {
    setMonth((current) => {
      const date = new Date(`${current}-01T12:00:00Z`);
      date.setUTCMonth(date.getUTCMonth() + delta);
      return date.toISOString().slice(0, 7);
    });
  }

  const selectedDay = shiftCivilDay(todayKey, offset);
  const agendaDays = view === "week" ? calendarWeek(selectedDay, settings.weekStartsOn) : [selectedDay];
  function openAgenda(mode: "day" | "week", dayOffset: number) {
    setOffset(dayOffset);
    setView(mode);
  }

  return (
    <div className="calendar-full" aria-label="Calendar">
      <CalendarDisplaySettings />
      <nav className="calendar-nav calendar-views" aria-label="Calendar views">
        <button type="button" aria-pressed={view === "month"} onClick={() => setView("month")}>Month</button>
        <button type="button" aria-pressed={view === "day" && offset === 0} onClick={() => openAgenda("day", 0)}>Today</button>
        <button type="button" aria-pressed={view === "day" && offset === 1} onClick={() => openAgenda("day", 1)}>Tomorrow</button>
        <button type="button" aria-pressed={view === "week" && offset === 0} onClick={() => openAgenda("week", 0)}>This week</button>
      </nav>
      {view !== "month" ? (
        <>
          <header className="calendar-head">
            <h1 className="calendar-title">{agendaDays[0]}{view === "week" ? ` – ${agendaDays[6]}` : ""}</h1>
            <div className="calendar-nav">
              <button type="button" onClick={() => setOffset(offset - (view === "week" ? 7 : 1))}>Previous {view}</button>
              <button type="button" onClick={() => setOffset(offset + (view === "week" ? 7 : 1))}>Next {view}</button>
            </div>
          </header>
          <CalendarAgendaFilter value={content} onChange={setContent} />
          <div hidden={content === "tasks"}>
            <CalendarEventsPanel days={agendaDays} defaultDate={selectedDay} timeGrid />
          </div>
          <div hidden={content === "events"}>
            {tasksQuery.isPending ? <p className="muted" role="status">Loading tasks...</p> : null}
            {tasksQuery.isError ? <p className="error" role="alert">Could not load tasks: {tasksQuery.error.message}</p> : null}
            {tasksQuery.data ? <CalendarAgenda tasks={tasksQuery.data.tasks} days={agendaDays} today={todayKey} /> : null}
          </div>
        </>
      ) : <>
      <header className="calendar-head">
        {monthNoteID !== undefined ? (
          <Link
            className="calendar-title"
            to="/notes/$noteId"
            params={{ noteId: monthNoteID }}
            title="Open month journal"
          >
            {monthLabel}
          </Link>
        ) : (
          <span className="calendar-title">{monthLabel}</span>
        )}
        <div className="calendar-nav">
          <button type="button" aria-label="Previous month" title="Previous month" onClick={() => shiftMonth(-1)}>
            <RailIcon Icon={IconChevronLeft} size={14} />
          </button>
          <button type="button" onClick={() => setMonth(currentMonth(zone))}>
            Current month
          </button>
          <button type="button" aria-label="Next month" title="Next month" onClick={() => shiftMonth(1)}>
            <RailIcon Icon={IconChevronRight} size={14} />
          </button>
        </div>
      </header>
      {notesQuery.isPending ? <p className="muted">Loading calendar...</p> : null}
      {notesQuery.isError ? <p className="error">{notesQuery.error.message}</p> : null}
      {tasksQuery.isError ? <p className="error" role="alert">Could not load tasks: {tasksQuery.error.message}</p> : null}
      {notesQuery.data ? (
        <div className="calendar-grid">
          {[...WEEKDAYS.slice(settings.weekStartsOn), ...WEEKDAYS.slice(0, settings.weekStartsOn)].map((day) => (
            <span className="calendar-weekday" key={day}>
              {day}
            </span>
          ))}
          {Array.from({ length: leadingBlanks }, (_, i) => (
            <span className="calendar-day calendar-day-blank" key={`blank-${i}`} />
          ))}
          {Array.from({ length: daysInMonth }, (_, i) => {
            const date = `${year}-${pad2(monthNo)}-${pad2(i + 1)}`;
            const dayNotes = notesByDay.get(date) ?? [];
            const today = date === todayKey ? "date" : undefined;
            const dayTasks = tasksByDay.get(date) ?? [];
            const dayEvents = (eventsQuery.data?.events ?? []).filter((event) => eventOnDay(event, date, zone));
            // A journal is titled by its date, so the map built for the month title finds the day's
            // one too. Clicking a date opens it when it exists — the day is what the journal is
            // about, so that is the page you meant. Navigation never creates: a day with no journal
            // keeps opening the day page, which is also the only thing a published site can do.
            const journalID = journals.get(`${year}${pad2(monthNo)}${pad2(i + 1)}`);
            if (dayNotes.length === 0 && dayTasks.length === 0 && dayEvents.length === 0 && journalID === undefined) {
              return (
                <span className="calendar-day" key={date} aria-current={today}>
                  <span className="calendar-day-number">{i + 1}</span>
                </span>
              );
            }
            const cell = (
              <>
                <span className="calendar-day-number">{i + 1}</span>
                {dayEvents.slice(0, 3).map((event) => <span className="calendar-day-note calendar-day-event" key={event.id}>{event.status === "cancelled" ? "Cancelled: " : ""}{event.title}</span>)}
                {dayEvents.length > 3 ? <span className="calendar-day-more">+{dayEvents.length - 3} events</span> : null}
                {dayTasks.slice(0, CELL_TASKS).map((task) => {
                  const bar = dueBar(task.due, todayKey);
                  return (
                    <span className="calendar-day-task" key={`${task.note_id}:${task.line}`}>
                      {/* The bar is a small data visualization — the days left until the due date,
                          urgency-scaled in the chart palette, the full track in --danger once
                          overdue. A task with only a scheduled date has no deadline to count down
                          to, so no bar. */}
                      {bar.kind !== "none" ? (
                        <span
                          className={`calendar-day-due${bar.kind === "overdue" ? " calendar-day-due-overdue" : ""}`}
                        >
                          <span className="calendar-day-due-fill" style={{ width: `${bar.fillPct}%` }} />
                        </span>
                      ) : null}
                      <span className="calendar-day-task-text">{task.text}</span>
                    </span>
                  );
                })}
                {dayTasks.length > CELL_TASKS ? (
                  <span className="calendar-day-more calendar-day-task-more">
                    +{dayTasks.length - CELL_TASKS}
                  </span>
                ) : null}
                {dayNotes.slice(0, CELL_NOTES).map((note) => (
                  <span className="calendar-day-note" key={note.note_id}>
                    {note.title}
                  </span>
                ))}
                {dayNotes.length > CELL_NOTES ? (
                  <span className="calendar-day-more">+{dayNotes.length - CELL_NOTES}</span>
                ) : null}
              </>
            );
            // Two literal links rather than one with a computed target: the router types the params
            // per route, so a shared spread would not typecheck.
            return journalID !== undefined && dayEvents.length === 0 ? (
              <Link
                className="calendar-day calendar-day-active"
                to="/notes/$noteId"
                params={{ noteId: String(journalID) }}
                key={date}
                aria-current={today}
                title={`Open the journal for ${date}`}
              >
                {cell}
              </Link>
            ) : (
              <Link
                className="calendar-day calendar-day-active"
                to="/day/$date"
                params={{ date }}
                key={date}
                aria-current={today}
                title={`Notes and tasks on ${date}`}
              >
                {cell}
              </Link>
            );
          })}
        </div>
      ) : null}
      <CalendarEventsPanel days={Array.from({ length: daysInMonth }, (_, i) => `${year}-${pad2(monthNo)}-${pad2(i + 1)}`)} defaultDate={todayKey.startsWith(`${year}-${pad2(monthNo)}-`) ? todayKey : `${year}-${pad2(monthNo)}-01`} />
      </>}
    </div>
  );
}

function currentMonth(zone: string): string {
  return calendarDateKey(Date.now(), zone).slice(0, 7);
}

function pad2(n: number): string {
  return String(n).padStart(2, "0");
}

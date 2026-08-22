import { Link } from "@tanstack/react-router";
import { useMemo, useState } from "react";
import { useDatedTasksQuery, useNotesQuery } from "../queries";
import type { NoteID, SearchResult, TaskRow } from "../types";
import { IconChevronLeft, IconChevronRight, RailIcon } from "./icons";

const WEEKDAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
// How many note titles a day cell lists before collapsing the rest into a "+N" count.
const CELL_NOTES = 3;

// CalendarFullView fills the reader with a month calendar of note activity. Each day cell lists the top
// notes active that day (from the notes listing's activity days) and links to the /day page, which shows
// the full list. Journals carry no activity days, so cells list only real notes; the month title still
// links to the yyyyMM summary journal when one exists. Everything derives from the notes list, which
// both the live server (/api/notes) and the static export (notes.json) provide, so the view needs no
// endpoint of its own — and no journals — to work.
export function CalendarFullView() {
  const notesQuery = useNotesQuery();
  // The calendar never waits on this: a pending or failed listing just means no task counts.
  const tasksQuery = useDatedTasksQuery();
  const [month, setMonth] = useState(startOfCurrentMonth);

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

  const year = month.getFullYear();
  const monthNo = month.getMonth() + 1;
  const daysInMonth = new Date(year, monthNo, 0).getDate();
  // `month` is the 1st, so its weekday is the number of leading blank cells (weeks start on Sunday).
  const leadingBlanks = month.getDay();
  const todayKey = dateKey(new Date());
  const monthLabel = `${year} / ${pad2(monthNo)}`;
  const monthNoteID = journals.get(`${year}${pad2(monthNo)}`);

  function shiftMonth(delta: number) {
    setMonth((current) => new Date(current.getFullYear(), current.getMonth() + delta, 1));
  }

  return (
    <div className="calendar-full" aria-label="Calendar">
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
          <button type="button" onClick={() => setMonth(startOfCurrentMonth())}>
            Today
          </button>
          <button type="button" aria-label="Next month" title="Next month" onClick={() => shiftMonth(1)}>
            <RailIcon Icon={IconChevronRight} size={14} />
          </button>
        </div>
      </header>
      {notesQuery.isPending ? <p className="muted">Loading calendar...</p> : null}
      {notesQuery.isError ? <p className="error">{notesQuery.error.message}</p> : null}
      {notesQuery.data ? (
        <div className="calendar-grid">
          {WEEKDAYS.map((day) => (
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
            // A journal is titled by its date, so the map built for the month title finds the day's
            // one too. Clicking a date opens it when it exists — the day is what the journal is
            // about, so that is the page you meant. Navigation never creates: a day with no journal
            // keeps opening the day page, which is also the only thing a published site can do.
            const journalID = journals.get(`${year}${pad2(monthNo)}${pad2(i + 1)}`);
            if (dayNotes.length === 0 && dayTasks.length === 0 && journalID === undefined) {
              return (
                <span className="calendar-day" key={date} aria-current={today}>
                  <span className="calendar-day-number">{i + 1}</span>
                </span>
              );
            }
            const cell = (
              <>
                <span className="calendar-day-number">{i + 1}</span>
                {dayNotes.slice(0, CELL_NOTES).map((note) => (
                  <span className="calendar-day-note" key={note.note_id}>
                    {note.title}
                  </span>
                ))}
                {dayNotes.length > CELL_NOTES ? (
                  <span className="calendar-day-more">+{dayNotes.length - CELL_NOTES}</span>
                ) : null}
                {/* One line, always last: a cell that already lists titles has no room to name tasks
                    too, and the count is what makes a planned day worth opening. */}
                {dayTasks.length > 0 ? (
                  <span className="calendar-day-tasks">
                    {dayTasks.length} task{dayTasks.length === 1 ? "" : "s"}
                  </span>
                ) : null}
              </>
            );
            // Two literal links rather than one with a computed target: the router types the params
            // per route, so a shared spread would not typecheck.
            return journalID !== undefined ? (
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
    </div>
  );
}

function startOfCurrentMonth(): Date {
  const now = new Date();
  return new Date(now.getFullYear(), now.getMonth(), 1);
}

function dateKey(date: Date): string {
  return `${date.getFullYear()}-${pad2(date.getMonth() + 1)}-${pad2(date.getDate())}`;
}

function pad2(n: number): string {
  return String(n).padStart(2, "0");
}

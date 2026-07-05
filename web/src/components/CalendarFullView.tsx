import { Link } from "@tanstack/react-router";
import { useMemo, useState } from "react";
import { useNotesQuery } from "../queries";
import type { NoteID, SearchResult } from "../types";

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
  const [month, setMonth] = useState(startOfCurrentMonth);

  // day (YYYY-MM-DD) → notes active that day, kept in the listing's order (most recently updated first
  // on the live server), so a cell's visible titles are the freshest ones.
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
            ‹
          </button>
          <button type="button" onClick={() => setMonth(startOfCurrentMonth())}>
            Today
          </button>
          <button type="button" aria-label="Next month" title="Next month" onClick={() => shiftMonth(1)}>
            ›
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
            if (dayNotes.length === 0) {
              return (
                <span className="calendar-day" key={date} aria-current={today}>
                  <span className="calendar-day-number">{i + 1}</span>
                </span>
              );
            }
            return (
              <Link
                className="calendar-day calendar-day-active"
                to="/day/$date"
                params={{ date }}
                key={date}
                aria-current={today}
                title={`Notes on ${date}`}
              >
                <span className="calendar-day-number">{i + 1}</span>
                {dayNotes.slice(0, CELL_NOTES).map((note) => (
                  <span className="calendar-day-note" key={note.note_id}>
                    {note.title}
                  </span>
                ))}
                {dayNotes.length > CELL_NOTES ? (
                  <span className="calendar-day-more">+{dayNotes.length - CELL_NOTES}</span>
                ) : null}
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

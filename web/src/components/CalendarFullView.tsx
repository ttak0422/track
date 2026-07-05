import { Link } from "@tanstack/react-router";
import { useMemo, useState } from "react";
import { useNotesQuery } from "../queries";
import type { NoteID } from "../types";

const WEEKDAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

// CalendarFullView fills the reader with a month calendar of day journals. A day whose yyyyMMdd journal
// exists links to it; the month title likewise links to the yyyyMM summary journal. Empty days are inert —
// creation stays with the rail's journal button and the activity heatmap, matching the heatmap's
// no-creation-path rule. Everything derives from the notes list, which both the live server (/api/notes)
// and the static export (notes.json) provide, so the view needs no endpoint of its own.
export function CalendarFullView() {
  const notesQuery = useNotesQuery();
  const [month, setMonth] = useState(startOfCurrentMonth);

  // Journal notes are date-addressed by title (yyyyMMdd days, yyyyMM months), so the title is the lookup
  // key directly — no date parsing needed.
  const journals = useMemo(() => {
    const map = new Map<string, NoteID>();
    for (const note of notesQuery.data?.notes ?? []) {
      if (note.file_kind === "journal") map.set(note.title, note.note_id);
    }
    return map;
  }, [notesQuery.data]);

  const monthKey = `${month.getFullYear()}${pad2(month.getMonth() + 1)}`;
  const daysInMonth = new Date(month.getFullYear(), month.getMonth() + 1, 0).getDate();
  // `month` is the 1st, so its weekday is the number of leading blank cells (weeks start on Sunday).
  const leadingBlanks = month.getDay();
  const todayKey = dayKey(new Date());
  const monthLabel = month.toLocaleString("en-US", { month: "long", year: "numeric" });
  const monthNoteID = journals.get(monthKey);

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
            title={`Open month journal ${monthKey}`}
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
            const key = `${monthKey}${pad2(i + 1)}`;
            const noteID = journals.get(key);
            const today = key === todayKey ? "date" : undefined;
            return noteID !== undefined ? (
              <Link
                className="calendar-day calendar-day-journal"
                to="/notes/$noteId"
                params={{ noteId: noteID }}
                key={key}
                aria-current={today}
                title={`Open journal ${key}`}
              >
                {i + 1}
              </Link>
            ) : (
              <span className="calendar-day" key={key} aria-current={today}>
                {i + 1}
              </span>
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

function dayKey(date: Date): string {
  return `${date.getFullYear()}${pad2(date.getMonth() + 1)}${pad2(date.getDate())}`;
}

function pad2(n: number): string {
  return String(n).padStart(2, "0");
}

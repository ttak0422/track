import { Link, useNavigate } from "@tanstack/react-router";
import { openJournal } from "../api";
import { useAgendaQuery, useResolveQuery } from "../queries";
import { STATIC_MODE } from "../runtime";

// DayView is the page a calendar day opens: the notes active that day (the same set the reader's
// "On this day" aside shows) as a plain list of links. The header offers the day's journal — the live
// workspace opens (creating if needed) it like the activity heatmap does; the static site links it only
// when the journal is published.
export function DayView({ date }: { date: string }) {
  const valid = /^\d{4}-\d{2}-\d{2}$/.test(date);
  const agendaQuery = useAgendaQuery(date, { enabled: valid });
  // Journal titles are the day's yyyyMMdd, so resolving that term finds the published journal.
  const journalQuery = useResolveQuery(STATIC_MODE && valid ? date.replaceAll("-", "") : "");
  const navigate = useNavigate();

  if (!valid) {
    return <p className="error">Invalid date: {date}</p>;
  }

  async function openDayJournal() {
    try {
      const { note_id } = await openJournal(date);
      navigate({ to: "/notes/$noteId", params: { noteId: String(note_id) } });
    } catch {
      // A failed open simply leaves the user on the current view.
    }
  }

  const journal = journalQuery.data?.found ? journalQuery.data.note : undefined;

  return (
    <div className="day-view" aria-label={`Notes on ${date}`}>
      <header className="day-head">
        <h1 className="day-title">{date}</h1>
        <div className="calendar-nav">
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
    </div>
  );
}

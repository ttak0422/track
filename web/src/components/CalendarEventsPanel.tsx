import { Link } from "@tanstack/react-router";
import { useRef, useState } from "react";
import { calendarEventOrder, eventFields, eventOnDay, eventTimeLabel, type CalendarEvent } from "../calendarEvents";
import { useCalendarEventsQuery, useSaveCalendarEvent } from "../calendarEventQueries";
import { useNotesQuery } from "../queries";
import { STATIC_MODE } from "../runtime";
import { useVaultScope } from "../vaultScope";
import { split } from "../vaultId";
import { CalendarEventDialog } from "./CalendarEventDialog";
import { useCalendarPreferences } from "../calendarPreferences";
import { CalendarTimeGrid } from "./CalendarTimeGrid";

interface PanelProps { days: string[]; defaultDate: string; timeGrid?: boolean }

export function CalendarEventsPanel(props: PanelProps) {
  // Event data is private to the live vault. Static exports keep their existing task snapshot.
  if (STATIC_MODE) return null;
  return <LiveCalendarEventsPanel {...props} />;
}

function LiveCalendarEventsPanel({ days, defaultDate, timeGrid }: PanelProps) {
  const { scope, activeName } = useVaultScope();
  return <ScopedEvents key={scope} vault={scope} vaultName={scope || activeName} timeGrid={timeGrid} days={days} defaultDate={defaultDate} />;
}

function ScopedEvents({ vault, vaultName, days, defaultDate, timeGrid }: PanelProps & { vault: string; vaultName: string }) {
  const { zone } = useCalendarPreferences();
  const events = useCalendarEventsQuery(vault);
  const notes = useNotesQuery();
  const save = useSaveCalendarEvent(vault);
  const [editing, setEditing] = useState<CalendarEvent | "new" | null>(null);
  const [trash, setTrash] = useState(false);
  const [layout, setLayout] = useState<"agenda" | "grid">("agenda");
  const [notice, setNotice] = useState("");
  const [undo, setUndo] = useState<CalendarEvent | null>(null);
  const [error, setError] = useState("");
  const busy = useRef(false);
  const restoreAttempt = useRef<{ fingerprint: string; operation: string } | null>(null);
  const rows = (events.data?.events ?? []).filter((event) => trash ? event.deleted : days.some((day) => eventOnDay(event, day, zone))).sort((a, b) => calendarEventOrder(a, b, zone));
  const localNotes = (notes.data?.notes ?? []).filter((note) => split(note.note_id).vault === vaultName);

  function saved(event: CalendarEvent) {
    setEditing(null);
    setNotice(event.deleted ? `Moved “${event.title}” to trash.` : `Saved “${event.title}”.`);
    if (event.deleted) setUndo(event);
  }

  async function restore(event: CalendarEvent) {
    if (busy.current) return;
    busy.current = true;
    setError("");
    const fingerprint = `${event.id}:${event.revision}`;
    if (restoreAttempt.current?.fingerprint !== fingerprint) restoreAttempt.current = { fingerprint, operation: crypto.randomUUID() };
    try {
      await save.mutateAsync({ id: event.id, request: { ...eventFields(event), deleted: false, expected_revision: event.revision, operation_id: restoreAttempt.current!.operation } });
      setUndo(null);
      setNotice(`Restored “${event.title}”.`);
    } catch (cause) { setError(cause instanceof Error ? cause.message : "Could not restore the event."); }
    finally { busy.current = false; }
  }

  return <section className="day-tasks calendar-events" aria-label="Calendar events">
    <header className="day-head">
      <h2>{trash ? "Deleted events" : "Events"}</h2>
      <div className="calendar-nav">
        <button type="button" onClick={(e) => { e.currentTarget.focus(); setEditing("new"); }}>New event</button>
        <button type="button" aria-pressed={trash} onClick={() => setTrash(!trash)}>{trash ? "Show events" : "Show trash"}</button>
      </div>
    </header>
    {timeGrid && !trash ? <div className="calendar-nav calendar-views" role="group" aria-label="Event layout">
      <button type="button" aria-pressed={layout === "agenda"} onClick={() => setLayout("agenda")}>Agenda</button>
      <button type="button" aria-pressed={layout === "grid"} onClick={() => setLayout("grid")}>Time grid</button>
    </div> : null}
    {notice ? <p role="status">{notice} {undo ? <button className="event-text-button" type="button" disabled={save.isPending} onClick={() => void restore(undo)}>Undo</button> : null}</p> : null}
    {error ? <p className="error" role="alert">{error}</p> : null}
    {events.isPending ? <p className="muted" role="status">Loading events...</p> : null}
    {events.isError ? <p className="error" role="alert">Could not load events: {events.error.message} <button className="event-text-button" type="button" onClick={() => void events.refetch()}>Retry events</button></p> : null}
    {events.data && rows.length === 0 ? <p className="muted">{trash ? "No deleted events." : "No events in this period."}</p> : null}
    {timeGrid && layout === "grid" && !trash && events.data ? <CalendarTimeGrid events={rows} days={days} onEdit={setEditing} /> : null}
    {(!timeGrid || layout === "agenda" || trash) ? rows.map((event) => <div className="calendar-event-row" key={event.id}>
      <div>
        {trash ? <strong>{event.title}</strong> : <button type="button" className="event-text-button event-title" onClick={(e) => { e.currentTarget.focus(); setEditing(event); }} aria-label={`Edit ${event.title}`}>{event.title}</button>}
        {event.status === "cancelled" ? <span className="day-task-note">Cancelled</span> : null}
        <div className="muted">{eventTimeLabel(event)}</div>
        {event.location ? <div className="muted">{event.location}</div> : null}
        {event.description ? <p className="calendar-event-description">{event.description}</p> : null}
        {event.note_id ? <Link to="/notes/$noteId" params={{ noteId: event.note_id }}>Related note</Link> : null}
        {event.url ? <> {event.note_id ? " · " : ""}<a href={event.url} target="_blank" rel="noreferrer">Source</a></> : null}
      </div>
      {trash ? <button className="event-text-button" type="button" disabled={save.isPending} onClick={() => void restore(event)}>Restore {event.title}</button> : null}
    </div>) : null}
    <p className="muted">{vaultName ? `Vault: ${vaultName}. ` : ""}Events use {zone} day boundaries; agenda labels show each event’s authored time zone.</p>
    {editing ? <CalendarEventDialog defaultTimeZone={zone} vault={vault} date={defaultDate} event={editing === "new" ? undefined : editing} notes={localNotes} onSaved={saved} onClose={() => setEditing(null)} /> : null}
  </section>;
}

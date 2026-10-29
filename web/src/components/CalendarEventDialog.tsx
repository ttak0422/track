import { useBlocker } from "@tanstack/react-router";
import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { browserSupportsTimeZone, eventFields, eventLocalParts, shiftCivilDay, type CalendarEvent, type CalendarEventFields, type CalendarEventWrite } from "../calendarEvents";
import { useSaveCalendarEvent } from "../calendarEventQueries";
import type { SearchResult } from "../types";
import { unsavedChangesMessage } from "../nativeNavigation";

function formSnapshot(element: HTMLDialogElement | null): string {
  return JSON.stringify(Array.from(element?.querySelectorAll<HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement>("input, textarea, select") ?? [],
    (field) => field instanceof HTMLInputElement && field.type === "checkbox" ? field.checked : field.value));
}

interface Props {
  vault: string;
  date: string;
  event?: CalendarEvent;
  notes: SearchResult[];
  onSaved: (event: CalendarEvent) => void;
  onClose: () => void;
}

export function CalendarEventDialog({ vault, date, event, notes, onSaved, onClose }: Props) {
  const dialog = useRef<HTMLDialogElement>(null);
  const titleInput = useRef<HTMLInputElement>(null);
  const id = useRef(event?.id ?? crypto.randomUUID());
  const busy = useRef(false);
  const released = useRef(false);
  const failed = useRef(false);
  const composing = useRef(false);
  const baseline = useRef("");
  const attempt = useRef<{ fingerprint: string; operation: string } | null>(null);
  const save = useSaveCalendarEvent(vault);
  const zone = event?.time_zone || Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  const initialStart: ReturnType<typeof eventLocalParts> = event && !event.all_day ? eventLocalParts(event.start, zone) : { date: event?.start ?? date, time: "09:00" };
  const initialEnd: ReturnType<typeof eventLocalParts> = event && !event.all_day ? eventLocalParts(event.end, zone) : { date: event ? shiftCivilDay(event.end, -1) : date, time: "10:00" };
  const [title, setTitle] = useState(event?.title ?? "");
  const [allDay, setAllDay] = useState(event?.all_day ?? true);
  const [startDate, setStartDate] = useState(initialStart.date);
  const [endDate, setEndDate] = useState(initialEnd.date);
  const [startTime, setStartTime] = useState(initialStart.time);
  const [endTime, setEndTime] = useState(initialEnd.time);
  const [timeZone, setTimeZone] = useState(zone);
  const initialStartOffset = event && !event.all_day ? event.start.match(/(Z|[+-]\d\d:\d\d)$/)?.[1] ?? "" : "";
  const initialEndOffset = event && !event.all_day ? event.end.match(/(Z|[+-]\d\d:\d\d)$/)?.[1] ?? "" : "";
  const [startOffset, setStartOffset] = useState(initialStartOffset);
  const [endOffset, setEndOffset] = useState(initialEndOffset);
  const [location, setLocation] = useState(event?.location ?? "");
  const [description, setDescription] = useState(event?.description ?? "");
  const [url, setURL] = useState(event?.url ?? "");
  const [noteID, setNoteID] = useState(event?.note_id ?? "");
  const [status, setStatus] = useState(event?.status ?? "confirmed");
  const [error, setError] = useState("");
  const [showOffsets, setShowOffsets] = useState(false);

  useLayoutEffect(() => {
    released.current = false;
    baseline.current = formSnapshot(dialog.current);
    return () => { released.current = true; };
  }, []);

  // Read the live controls: IME and the last input event can precede React's commit.
  const protectedDraft = useCallback(() => !released.current && (busy.current || failed.current || composing.current || formSnapshot(dialog.current) !== baseline.current), []);
  useBlocker({
    shouldBlockFn: ({ current, next }) => {
      if (current.pathname === next.pathname) return false;
      if (protectedDraft() && !window.confirm(unsavedChangesMessage)) return true;
      released.current = true;
      onClose(); // Day-route parameter changes can keep the same panel mounted.
      return false;
    },
    enableBeforeUnload: false,
  });
  useEffect(() => {
    // History.back() suppresses the router's next beforeunload, even after a cancelled Back.
    // Keep the form's unload check independent so a subsequent reload still protects its draft.
    const beforeUnload = (event: BeforeUnloadEvent) => {
      if (!protectedDraft()) return;
      event.preventDefault();
      event.returnValue = "";
    };
    window.addEventListener("beforeunload", beforeUnload);
    return () => window.removeEventListener("beforeunload", beforeUnload);
  }, [protectedDraft]);

  useEffect(() => {
    const element = dialog.current;
    const previous = document.activeElement;
    element?.showModal();
    titleInput.current?.focus();
    return () => {
      element?.close();
      if (previous instanceof HTMLElement && previous.isConnected) previous.focus();
    };
  }, []);

  function close() {
    if (!busy.current) {
      released.current = true;
      onClose();
    }
  }

  async function submit(deleted = false) {
    if (busy.current || released.current) return;
    const localTime = (date: string, time: string, offset: string) => `${date}T${time.length === 5 ? `${time}:00` : time}${offset}`;
    const unchangedStart = event && !event.all_day && startDate === initialStart.date && startTime === initialStart.time && timeZone === zone && startOffset === initialStartOffset;
    const unchangedEnd = event && !event.all_day && endDate === initialEnd.date && endTime === initialEnd.time && timeZone === zone && endOffset === initialEndOffset;
    if (!deleted && !allDay && (!unchangedStart || !unchangedEnd) && !browserSupportsTimeZone(timeZone.trim())) {
      setError(`Time zone “${timeZone.trim()}” is unavailable in this browser. Choose a supported IANA time zone before changing dates or times.`);
      return;
    }
    const fields: CalendarEventFields = deleted && event ? { ...eventFields(event), deleted: true } : {
      title: title.trim(), all_day: allDay,
      start: allDay ? startDate : unchangedStart ? event!.start : localTime(startDate, startTime, startOffset),
      end: allDay ? shiftCivilDay(endDate, 1) : unchangedEnd ? event!.end : localTime(endDate, endTime, endOffset),
      time_zone: allDay ? "" : timeZone.trim(), description, location, url: url.trim(), note_id: noteID, status, deleted,
    };
    // Keep the same operation for an unchanged retry after an uncertain network response.
    const fingerprint = JSON.stringify(fields);
    if (attempt.current?.fingerprint !== fingerprint) attempt.current = { fingerprint, operation: crypto.randomUUID() };
    const request: CalendarEventWrite = { ...fields, expected_revision: event?.revision ?? 0, operation_id: attempt.current!.operation };
    busy.current = true;
    failed.current = false;
    setError("");
    try {
      const result = await save.mutateAsync({ id: id.current, request });
      if (released.current) return;
      released.current = true;
      onSaved(result.event);
    } catch (cause) {
      if (released.current) return;
      failed.current = true;
      const message = cause instanceof Error ? cause.message : "Could not save the event.";
      setError(message);
      if (message.toLowerCase().includes("ambiguous")) setShowOffsets(true);
    } finally { busy.current = false; }
  }

  return <dialog ref={dialog} className="modal-card calendar-event-dialog" aria-labelledby="calendar-event-title"
    onCancel={(e) => { e.preventDefault(); close(); }}>
    <h3 id="calendar-event-title">{event ? "Edit event" : "New event"}</h3>
    {event && !event.all_day && (initialStart.timeZoneUnavailable || initialEnd.timeZoneUnavailable) ?
      <p role="alert">Time zone “{zone}” is unavailable in this browser. Showing the saved local clock and UTC offsets. Choose a supported IANA time zone to change dates or times; other edits preserve the saved timestamps.</p> : null}
    <form onSubmit={(e) => { e.preventDefault(); void submit(); }}
      onCompositionStartCapture={() => { composing.current = true; }} onCompositionEndCapture={() => { composing.current = false; }}>
      <fieldset disabled={save.isPending}>
        <label className="modal-field">Title<input ref={titleInput} className="modal-input" required maxLength={256} value={title} onChange={(e) => setTitle(e.target.value)} /></label>
        <label><input type="checkbox" checked={allDay} onChange={(e) => { setAllDay(e.target.checked); setStartOffset(""); setEndOffset(""); }} /> All day</label>
        <div className="calendar-event-dates">
          <label className="modal-field">Starts on<input className="modal-input" type="date" required value={startDate} onChange={(e) => { setStartDate(e.target.value); setStartOffset(""); }} onInput={(e) => { setStartDate(e.currentTarget.value); setStartOffset(""); }} /></label>
          {!allDay ? <label className="modal-field">Start time<input className="modal-input" type="time" step="1" required value={startTime} onChange={(e) => { setStartTime(e.target.value); setStartOffset(""); }} onInput={(e) => { setStartTime(e.currentTarget.value); setStartOffset(""); }} /></label> : null}
          <label className="modal-field">{allDay ? "Last day" : "Ends on"}<input className="modal-input" type="date" required value={endDate} onChange={(e) => { setEndDate(e.target.value); setEndOffset(""); }} onInput={(e) => { setEndDate(e.currentTarget.value); setEndOffset(""); }} /></label>
          {!allDay ? <label className="modal-field">End time<input className="modal-input" type="time" step="1" required value={endTime} onChange={(e) => { setEndTime(e.target.value); setEndOffset(""); }} onInput={(e) => { setEndTime(e.currentTarget.value); setEndOffset(""); }} /></label> : null}
        </div>
        {!allDay ? <>
          <label className="modal-field">Time zone<input className="modal-input" required value={timeZone} placeholder="Asia/Tokyo" onChange={(e) => { setTimeZone(e.target.value); setStartOffset(""); setEndOffset(""); }} /></label>
          <details open={showOffsets} onToggle={(e) => setShowOffsets(e.currentTarget.open)}><summary>UTC offsets for repeated times</summary>
            <p className="muted">Use explicit offsets when a clock change repeats a local time, for example -04:00 or -05:00. Leave blank otherwise.</p>
            <label className="modal-field">Start UTC offset<input className="modal-input" value={startOffset} placeholder="-04:00" onChange={(e) => setStartOffset(e.target.value)} /></label>
            <label className="modal-field">End UTC offset<input className="modal-input" value={endOffset} placeholder="-05:00" onChange={(e) => setEndOffset(e.target.value)} /></label>
          </details>
        </> : null}
        <label className="modal-field">Location<input className="modal-input" value={location} onChange={(e) => setLocation(e.target.value)} /></label>
        <label className="modal-field">Description<textarea className="modal-input" rows={3} value={description} onChange={(e) => setDescription(e.target.value)} /></label>
        <label className="modal-field">Source URL<input className="modal-input" type="url" value={url} onChange={(e) => setURL(e.target.value)} /></label>
        <label className="modal-field">Related note<select className="modal-input" value={noteID} onChange={(e) => setNoteID(e.target.value)}><option value="">None</option>{noteID && !notes.some((note) => note.note_id === noteID) ? <option value={noteID}>Note {noteID}</option> : null}{notes.map((note) => <option key={note.note_id} value={note.note_id}>{note.title}</option>)}</select></label>
        <label className="modal-field">Status<select className="modal-input" value={status} onChange={(e) => setStatus(e.target.value as "confirmed" | "cancelled")}><option value="confirmed">Confirmed</option><option value="cancelled">Cancelled</option></select></label>
      </fieldset>
      {error ? <p className="error" role="alert">{error} Your input is kept. If another editor changed this event, close and reopen it before retrying.</p> : null}
      <div className="modal-actions">
        {event ? <button type="button" disabled={save.isPending} onClick={() => void submit(true)}>Move to trash</button> : null}
        <button type="button" disabled={save.isPending} onClick={close}>Cancel</button>
        <button type="submit" disabled={save.isPending || !title.trim()}>{save.isPending ? "Saving..." : "Save event"}</button>
      </div>
    </form>
  </dialog>;
}

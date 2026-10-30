import { useEffect, useState } from "react";
import { CalendarTimeEntry } from "./CalendarTimeEntry";
import { eventOnDay, eventTimeLabel, type CalendarEvent } from "../calendarEvents";
import { useCalendarPreferences } from "../calendarPreferences";
import { gridClock, localDayRange, timedSegments } from "./eventTimeLayout";

export function CalendarTimeGrid({ events, days, onEdit, onTimeChange }: { events: CalendarEvent[]; days: string[]; onEdit: (event: CalendarEvent) => void; onTimeChange?: (original: CalendarEvent, proposal: CalendarEvent) => void }) {
  const { zone } = useCalendarPreferences();
  const [notice, setNotice] = useState("");
  const period = days.join("|");
  useEffect(() => setNotice(""), [period, zone]);
  const columns = days.map((day) => ({ day, range: localDayRange(day, zone), segments: timedSegments(events, day, zone) }));
  return <>
    <p className="muted">Time grid · {zone}. Each column follows elapsed time from midnight in the display zone; daylight-saving days may have fewer or more hours. Tasks stay in their dated agenda.</p>
    {onTimeChange ? <p className="muted">On ordinary 24-hour days, drag a confirmed single-day event to move it; drag ↕ to resize its end. Activate Change time for the same 15-minute actions by keyboard. Review and Apply before saving. Use the editor for all-day, cancelled, clock-change dates or events crossing a display-zone day boundary.</p> : null}
    {notice ? <p role="status">{notice}</p> : null}
    <div className="calendar-time-scroll" tabIndex={0} role="region" aria-label="Event time grid, scroll for more days and hours">
      <div className="calendar-time-grid" style={{ gridTemplateColumns: columns.map(({ segments }) => `minmax(${Math.max(240, 64 + Math.max(1, ...segments.map((segment) => segment.columns)) * 108)}px, 1fr)`).join(" ") }}>
        {days.map((day) => <h3 className="calendar-time-heading" key={day}>{day}</h3>)}
        {days.map((day) => <section className="calendar-time-all-day" aria-label={`All-day events on ${day}`} key={day}>
          <span className="muted">All day</span>
          {events.filter((event) => event.all_day && eventOnDay(event, day, zone)).map((event) => <button type="button" className="calendar-time-event calendar-time-all-day-event" key={event.id} onClick={(e) => { e.currentTarget.focus(); onEdit(event); }} aria-label={`Edit ${event.title} on ${day}: ${event.status === "cancelled" ? "Cancelled · " : ""}${eventTimeLabel(event)}`}>
            {event.status === "cancelled" ? "Cancelled · " : ""}{event.title}
          </button>)}
        </section>)}
        {columns.map(({ day, range, segments }) => {
          const transition = range.minutes !== 1440;
          return <section className="calendar-time-day" aria-label={`Timed events on ${day}`} key={day} style={{ height: `${range.minutes + 24}px` }}>
            {Array.from({ length: Math.ceil(range.minutes / 60) }, (_, hour) => <span className="calendar-time-tick" aria-hidden="true" key={hour} style={{ top: `${hour * 60}px` }}>{gridClock(range.start + hour * 3_600_000, transition, zone)}</span>)}
            {range.minutes === 0 ? <p className="muted">No local hours on this date.</p> : null}
            {segments.length === 0 && range.minutes > 0 ? <p className="calendar-time-empty muted">No timed events.</p> : null}
            <div className="calendar-time-entries">
              {segments.map((segment) => <CalendarTimeEntry key={segment.event.id} segment={segment} day={day} zone={zone} onEdit={onEdit} onTimeChange={onTimeChange} onNotice={setNotice} />)}
            </div>
          </section>;
        })}
      </div>
    </div>
  </>;
}

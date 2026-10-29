import { eventOnDay, eventTimeLabel, type CalendarEvent } from "../calendarEvents";
import { gridClock, localDayRange, timedSegments } from "./eventTimeLayout";

export function CalendarTimeGrid({ events, days, onEdit }: { events: CalendarEvent[]; days: string[]; onEdit: (event: CalendarEvent) => void }) {
  const columns = days.map((day) => ({ day, range: localDayRange(day), segments: timedSegments(events, day) }));
  const zone = Intl.DateTimeFormat().resolvedOptions().timeZone;
  return <>
    <p className="muted">Time grid · {zone}. Each column follows elapsed time from local midnight; daylight-saving days may have fewer or more hours. Tasks stay in their dated agenda.</p>
    <div className="calendar-time-scroll" tabIndex={0} role="region" aria-label="Event time grid, scroll for more days and hours">
      <div className="calendar-time-grid" style={{ gridTemplateColumns: columns.map(({ segments }) => `minmax(${Math.max(240, 64 + Math.max(1, ...segments.map((segment) => segment.columns)) * 108)}px, 1fr)`).join(" ") }}>
        {days.map((day) => <h3 className="calendar-time-heading" key={day}>{day}</h3>)}
        {days.map((day) => <section className="calendar-time-all-day" aria-label={`All-day events on ${day}`} key={day}>
          <span className="muted">All day</span>
          {events.filter((event) => event.all_day && eventOnDay(event, day)).map((event) => <button type="button" className="calendar-time-event calendar-time-all-day-event" key={event.id} onClick={(e) => { e.currentTarget.focus(); onEdit(event); }} aria-label={`Edit ${event.title} on ${day}: ${eventTimeLabel(event)}`}>
            {event.status === "cancelled" ? "Cancelled · " : ""}{event.title}
          </button>)}
        </section>)}
        {columns.map(({ day, range, segments }) => {
          const transition = range.minutes !== 1440;
          return <section className="calendar-time-day" aria-label={`Timed events on ${day}`} key={day} style={{ height: `${range.minutes + 24}px` }}>
            {Array.from({ length: Math.ceil(range.minutes / 60) }, (_, hour) => <span className="calendar-time-tick" aria-hidden="true" key={hour} style={{ top: `${hour * 60}px` }}>{gridClock(range.start + hour * 3_600_000, transition)}</span>)}
            {range.minutes === 0 ? <p className="muted">No local hours on this date.</p> : null}
            {segments.length === 0 && range.minutes > 0 ? <p className="calendar-time-empty muted">No timed events.</p> : null}
            <div className="calendar-time-entries">
              {segments.map(({ event, start, end, column, columns: count }) => <button type="button" key={event.id} className="calendar-time-event" onClick={(e) => { e.currentTarget.focus(); onEdit(event); }}
                aria-label={`Edit ${event.title} on ${day}: ${eventTimeLabel(event)}`}
                title={eventTimeLabel(event)}
                style={{ top: `${start}px`, height: `${Math.max(24, end - start)}px`, left: `${column * 100 / count}%`, width: `${100 / count}%` }}>
                <span>{event.status === "cancelled" ? "Cancelled · " : ""}{event.title}</span>
                <small>{Date.parse(event.start) < range.start ? "Continues · " : ""}{gridClock(range.start + start * 60_000, transition)}–{end === range.minutes ? "24:00" : gridClock(range.start + end * 60_000, transition)}{Date.parse(event.end) > range.end ? " · continues" : ""}</small>
              </button>)}
            </div>
          </section>;
        })}
      </div>
    </div>
  </>;
}

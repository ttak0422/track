import { eventOnDay, type CalendarEvent } from "../calendarEvents";
import { calendarDayRange, calendarClock, deviceTimeZone } from "../calendarDates";

export interface TimedSegment {
  event: CalendarEvent;
  start: number;
  end: number;
  column: number;
  columns: number;
}

export { calendarDayRange as localDayRange } from "../calendarDates";

// Elapsed minutes retain both occurrences of a repeated DST hour. Each day labels its own clock.
export function timedSegments(events: CalendarEvent[], day: string, zone = deviceTimeZone()): TimedSegment[] {
  const range = calendarDayRange(day, zone);
  const segments = events.filter((event) => !event.all_day && eventOnDay(event, day, zone)).map((event) => ({
    event,
    start: (Math.max(Date.parse(event.start), range.start) - range.start) / 60_000,
    end: (Math.min(Date.parse(event.end), range.end) - range.start) / 60_000,
    column: 0, columns: 1,
  })).filter((segment) => segment.end > segment.start).sort((a, b) => a.start - b.start || b.end - a.end || a.event.id.localeCompare(b.event.id));
  let group: TimedSegment[] = [];
  let ends: number[] = [];
  let groupEnd = -1;
  const finish = () => { for (const segment of group) segment.columns = ends.length; };
  for (const segment of segments) {
    if (segment.start >= groupEnd) { finish(); group = []; ends = []; }
    let column = ends.findIndex((end) => end <= segment.start);
    if (column < 0) column = ends.length;
    // A short event still needs a keyboard/pointer target. Include its drawn height in collision
    // detection so adjacent short entries never paint over each other.
    const visualEnd = Math.max(segment.end, segment.start + 24);
    ends[column] = visualEnd;
    segment.column = column;
    group.push(segment);
    groupEnd = Math.max(...ends);
  }
  finish();
  return segments;
}

export function gridClock(instant: number, offset = false, zone = deviceTimeZone()): string {
  return calendarClock(instant, zone, offset);
}

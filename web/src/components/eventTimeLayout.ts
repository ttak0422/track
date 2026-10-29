import { eventOnDay, type CalendarEvent } from "../calendarEvents";

export interface TimedSegment {
  event: CalendarEvent;
  start: number;
  end: number;
  column: number;
  columns: number;
}

export function localDayRange(day: string): { start: number; end: number; minutes: number } {
  const start = new Date(`${day}T00:00:00`);
  const end = new Date(start);
  end.setDate(end.getDate() + 1);
  // A skipped civil day (for example Apia 2011-12-30) has no timed interval.
  const exists = start.getFullYear() === Number(day.slice(0, 4)) && start.getMonth() + 1 === Number(day.slice(5, 7)) && start.getDate() === Number(day.slice(8, 10));
  return { start: +start, end: exists ? +end : +start, minutes: exists ? (+end - +start) / 60_000 : 0 };
}

// Elapsed minutes retain both occurrences of a repeated DST hour. Each day labels its own clock.
export function timedSegments(events: CalendarEvent[], day: string): TimedSegment[] {
  const range = localDayRange(day);
  const segments = events.filter((event) => !event.all_day && eventOnDay(event, day)).map((event) => ({
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

export function gridClock(instant: number, offset = false): string {
  const date = new Date(instant);
  const clock = `${String(date.getHours()).padStart(2, "0")}:${String(date.getMinutes()).padStart(2, "0")}`;
  if (!offset) return clock;
  const minutes = -date.getTimezoneOffset();
  return `${clock} UTC${minutes >= 0 ? "+" : "−"}${String(Math.floor(Math.abs(minutes) / 60)).padStart(2, "0")}:${String(Math.abs(minutes) % 60).padStart(2, "0")}`;
}

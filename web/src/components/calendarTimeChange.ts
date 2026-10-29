import { browserSupportsTimeZone, eventLocalParts, type CalendarEvent } from "../calendarEvents";
import { calendarDayRange } from "../calendarDates";

export type TimeChangeMode = "move" | "resize-end";

// Whole-minute adjustments retain the authored zone and the original fractional precision.
function shiftedTimestamp(instant: number, original: string, zone: string): string | null {
  const parts = eventLocalParts(new Date(instant).toISOString(), zone);
  if (parts.timeZoneUnavailable) return null;
  const second = instant - ((instant % 1000 + 1000) % 1000);
  const offset = (Date.parse(`${parts.date}T${parts.time}Z`) - second) / 60_000;
  if (!Number.isInteger(offset) || Math.abs(offset) >= 1440) return null;
  const suffix = offset === 0 ? "Z" : `${offset < 0 ? "-" : "+"}${String(Math.floor(Math.abs(offset) / 60)).padStart(2, "0")}:${String(Math.abs(offset) % 60).padStart(2, "0")}`;
  const fraction = original.match(/(\.\d+)(?:Z|[+-]\d\d:\d\d)$/)?.[1] ?? "";
  const value = `${parts.date}T${parts.time}${fraction}${suffix}`;
  return Date.parse(value) === instant ? value : null;
}

export function canChangeTime(event: CalendarEvent, day: string, zone: string): boolean {
  const range = calendarDayRange(day, zone);
  const start = Date.parse(event.start), end = Date.parse(event.end);
  return !event.all_day && !event.deleted && event.status === "confirmed" && range.minutes === 1440 &&
    Number.isFinite(start) && end > start && start >= range.start && end <= range.end &&
    browserSupportsTimeZone(event.time_zone) && shiftedTimestamp(start, event.start, event.time_zone) !== null && shiftedTimestamp(end, event.end, event.time_zone) !== null;
}

export function proposeTimeChange(event: CalendarEvent, day: string, zone: string, mode: TimeChangeMode, minutes: number): CalendarEvent | null {
  if (!canChangeTime(event, day, zone) || !Number.isInteger(minutes) || minutes === 0 || minutes % 15 !== 0) return null;
  const range = calendarDayRange(day, zone);
  const start = Date.parse(event.start) + (mode === "move" ? minutes * 60_000 : 0);
  const end = Date.parse(event.end) + minutes * 60_000;
  if (start < range.start || end > range.end || end <= start || (mode === "resize-end" && end - start < 15 * 60_000)) return null;
  const newStart = mode === "move" ? shiftedTimestamp(start, event.start, event.time_zone) : event.start;
  const newEnd = shiftedTimestamp(end, event.end, event.time_zone);
  return newStart && newEnd ? { ...event, start: newStart, end: newEnd } : null;
}

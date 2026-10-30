import { calendarDateKey, calendarDayRange, shiftCivilDay } from "./calendarDates";
export { shiftCivilDay } from "./calendarDates";
import { api } from "./api";
import { STATIC_MODE } from "./runtime";
import { split, vaultParams } from "./vaultId";

export interface CalendarEventFields {
  title: string;
  all_day: boolean;
  start: string;
  end: string;
  time_zone: string;
  description: string;
  location: string;
  url: string;
  note_id: string;
  status: "confirmed" | "cancelled";
  deleted: boolean;
}

export interface CalendarEvent extends CalendarEventFields {
  version: number;
  id: string;
  revision: number;
  updated_at: string;
}

export interface CalendarEventWrite extends CalendarEventFields {
  expected_revision: number;
  operation_id: string;
}

export function eventFields(event: CalendarEventFields): CalendarEventFields {
  const { title, all_day, start, end, time_zone, description, location, url, note_id, status, deleted } = event;
  return { title, all_day, start, end, time_zone, description, location, url, note_id, status, deleted };
}

export function listCalendarEvents(vault: string): Promise<{ events: CalendarEvent[] }> {
  if (STATIC_MODE) return Promise.resolve({ events: [] });
  return api<{ events: CalendarEvent[] }>(`/api/calendar-events?deleted=1${vaultParams(vault)}`).then((result) => ({ events: result.events.map(normalizeEvent) }));
}

function normalizeEvent(event: CalendarEvent): CalendarEvent {
  return { ...event, note_id: split(event.note_id).id ? event.note_id : "" };
}

export async function saveCalendarEvent(vault: string, id: string, request: CalendarEventWrite, noteVault = vault): Promise<{ event: CalendarEvent }> {
  if (STATIC_MODE) throw new Error("Published calendars are read-only.");
  const related = split(request.note_id);
  if (related.vault && related.vault !== noteVault) throw new Error("The related note must belong to the event’s vault.");
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 30_000);
  try {
    const result = await api<{ event: CalendarEvent }>(`/api/calendar-event?id=${encodeURIComponent(id)}${vaultParams(vault)}`, {
      method: "PUT", signal: controller.signal,
      body: { ...eventFields(request), expected_revision: request.expected_revision, operation_id: request.operation_id, note_id: related.id },
    });
    return { event: normalizeEvent(result.event) };
  } catch (cause) {
    if (controller.signal.aborted) throw new Error("Save confirmation timed out. The save may have completed; retry unchanged to check it safely.");
    throw cause;
  } finally { clearTimeout(timer); }
}

// All-day dates are civil days. Timed membership follows the selected display zone, including
// 23/25-hour DST days, while the row labels the event's own time zone explicitly.
export function eventOnDay(event: CalendarEvent, day: string, timeZone?: string): boolean {
  if (event.deleted) return false;
  if (event.all_day) return event.start <= day && day < event.end;
  const { start, end } = calendarDayRange(day, timeZone);
  return end > start && Date.parse(event.start) < end && Date.parse(event.end) > start;
}

export function browserSupportsTimeZone(zone: string): boolean {
  try { new Intl.DateTimeFormat("en", { timeZone: zone }); return true; }
  catch { return false; }
}

export function eventLocalParts(value: string, zone: string): { date: string; time: string; timeZoneUnavailable?: boolean } {
  try {
    const parts = new Intl.DateTimeFormat("en-CA", {
      timeZone: zone, year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", second: "2-digit", hourCycle: "h23",
    }).formatToParts(new Date(value));
    const part = (name: string) => parts.find((item) => item.type === name)?.value ?? "";
    return { date: `${part("year").padStart(4, "0")}-${part("month")}-${part("day")}`, time: `${part("hour")}:${part("minute")}:${part("second")}` };
  } catch {
    // Saved RFC3339 values already carry the authored local clock and offset.
    // Keep those values visible if this browser's zone database cannot format it.
    return { date: value.slice(0, 10), time: value.slice(11, 19), timeZoneUnavailable: true };
  }
}

export function eventTimeLabel(event: CalendarEvent): string {
  if (event.all_day) {
    const last = shiftCivilDay(event.end, -1);
    return `All day · ${event.start}${last === event.start ? "" : ` – ${last}`}`;
  }
  const start = eventLocalParts(event.start, event.time_zone);
  const end = eventLocalParts(event.end, event.time_zone);
  if (start.timeZoneUnavailable || end.timeZoneUnavailable) {
    return `${event.start} – ${event.end} · Time zone “${event.time_zone}” is unavailable in this browser.`;
  }
  return `${start.date} ${start.time} – ${end.date} ${end.time} · ${event.time_zone}`;
}

export function calendarEventOrder(a: CalendarEvent, b: CalendarEvent, timeZone?: string): number {
  // Compare instants across authored zones; civil grouping follows the selected display zone.
  const day = (event: CalendarEvent) => event.all_day ? event.start : calendarDateKey(Date.parse(event.start), timeZone);
  return day(a).localeCompare(day(b)) || Number(b.all_day) - Number(a.all_day) ||
    (!a.all_day && !b.all_day ? Date.parse(a.start) - Date.parse(b.start) : 0) || a.title.localeCompare(b.title) || a.id.localeCompare(b.id);
}

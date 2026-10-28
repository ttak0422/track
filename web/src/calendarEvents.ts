import { api } from "./api";
import { STATIC_MODE } from "./runtime";
import { split, vaultParams } from "./vaultId";
import { shiftDay } from "./components/activityDates";

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

// All-day dates are civil days. Timed membership follows device-local day boundaries, including
// 23/25-hour DST days, while the row labels the event's own time zone explicitly.
export function eventOnDay(event: CalendarEvent, day: string): boolean {
  if (event.deleted) return false;
  if (event.all_day) return event.start <= day && day < event.end;
  const start = new Date(`${day}T00:00:00`);
  const end = new Date(start);
  end.setDate(end.getDate() + 1);
  return new Date(event.start) < end && new Date(event.end) > start;
}

export function eventLocalParts(value: string, zone: string): { date: string; time: string } {
  const parts = new Intl.DateTimeFormat("en-CA", {
    timeZone: zone, year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", second: "2-digit", hourCycle: "h23",
  }).formatToParts(new Date(value));
  const part = (name: string) => parts.find((item) => item.type === name)?.value ?? "";
  return { date: `${part("year").padStart(4, "0")}-${part("month")}-${part("day")}`, time: `${part("hour")}:${part("minute")}:${part("second")}` };
}

export function eventTimeLabel(event: CalendarEvent): string {
  if (event.all_day) {
    const last = shiftDay(event.end, -1);
    return `All day · ${event.start}${last === event.start ? "" : ` – ${last}`}`;
  }
  const start = eventLocalParts(event.start, event.time_zone);
  const end = eventLocalParts(event.end, event.time_zone);
  return `${start.date} ${start.time} – ${end.date} ${end.time} · ${event.time_zone}`;
}

export function calendarEventOrder(a: CalendarEvent, b: CalendarEvent): number {
  // Compare instants across zones; the device-local date is the calendar's grouping key.
  const day = (event: CalendarEvent) => event.all_day ? event.start : new Date(event.start).toLocaleDateString("sv-SE");
  return day(a).localeCompare(day(b)) || Number(b.all_day) - Number(a.all_day) ||
    (!a.all_day && !b.all_day ? Date.parse(a.start) - Date.parse(b.start) : 0) || a.title.localeCompare(b.title) || a.id.localeCompare(b.id);
}

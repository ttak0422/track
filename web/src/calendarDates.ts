const dayFormatters = new Map<string, Intl.DateTimeFormat>();
const dayRanges = new Map<string, { start: number; end: number; minutes: number }>();

export function deviceTimeZone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
}

export function calendarDateKey(instant: number | Date, zone = deviceTimeZone()): string {
  let formatter = dayFormatters.get(zone);
  if (!formatter) {
    formatter = new Intl.DateTimeFormat("en", { timeZone: zone, year: "numeric", month: "2-digit", day: "2-digit" });
    if (dayFormatters.size >= 32) dayFormatters.clear();
    dayFormatters.set(zone, formatter);
  }
  const parts = formatter.formatToParts(instant);
  const part = (name: string) => parts.find((value) => value.type === name)?.value ?? "";
  return `${part("year").padStart(4, "0")}-${part("month")}-${part("day")}`;
}

// Civil arithmetic must retain even dates skipped by a zone, such as Apia 2011-12-30.
export function shiftCivilDay(day: string, amount: number): string {
  const date = new Date(`${day}T00:00:00Z`);
  date.setUTCDate(date.getUTCDate() + amount);
  return `${String(date.getUTCFullYear()).padStart(4, "0")}-${String(date.getUTCMonth() + 1).padStart(2, "0")}-${String(date.getUTCDate()).padStart(2, "0")}`;
}

export function isCalendarDay(day: string): boolean {
  const instant = Date.parse(`${day}T00:00:00Z`);
  return /^\d{4}-\d{2}-\d{2}$/.test(day) && Number.isFinite(instant) && new Date(instant).toISOString().slice(0, 10) === day;
}

export function calendarDayRange(day: string, zone = deviceTimeZone()): { start: number; end: number; minutes: number } {
  if (!isCalendarDay(day)) return { start: 0, end: 0, minutes: 0 };
  const key = `${zone}:${day}`;
  const cached = dayRanges.get(key);
  if (cached) return cached;
  // Find each civil boundary independently. Midnight gaps normalize to the first actual instant;
  // skipped dates have the same start/end. No fixed 24-hour duration or carried normalized hour.
  const boundary = (date: string) => {
    const midnight = Date.parse(`${date}T00:00:00Z`);
    let low = midnight - 172_800_000;
    let high = midnight + 172_800_000;
    while (high - low > 1) {
      const middle = Math.floor((low + high) / 2);
      if (calendarDateKey(middle, zone) < date) low = middle;
      else high = middle;
    }
    return high;
  };
  const start = boundary(day);
  const end = boundary(shiftCivilDay(day, 1));
  const range = { start, end, minutes: (end - start) / 60_000 };
  if (dayRanges.size >= 128) dayRanges.clear();
  dayRanges.set(key, range);
  return range;
}

export function calendarClock(instant: number, zone: string, offset = false): string {
  const parts = new Intl.DateTimeFormat("en", { timeZone: zone, hour: "2-digit", minute: "2-digit", hourCycle: "h23", timeZoneName: "shortOffset" }).formatToParts(instant);
  const part = (name: string) => parts.find((value) => value.type === name)?.value ?? "";
  const clock = `${part("hour")}:${part("minute")}`;
  if (!offset) return clock;
  const match = part("timeZoneName").match(/^(?:GMT|UTC)(?:([+-])(\d{1,2})(?::(\d{2}))?)?$/);
  if (!match) return `${clock} ${part("timeZoneName")}`;
  return `${clock} UTC${match[1] === "-" ? "−" : "+"}${(match[2] ?? "0").padStart(2, "0")}:${match[3] ?? "00"}`;
}

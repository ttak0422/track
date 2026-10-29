import { expect, it } from "vitest";
import type { CalendarEvent } from "../calendarEvents";
import { gridClock, localDayRange, timedSegments } from "./eventTimeLayout";

const event = (id: string, start: string, end: string, extra: Partial<CalendarEvent> = {}): CalendarEvent => ({ version: 1, id, revision: 1, title: id, all_day: false, start, end, time_zone: "UTC", description: "", location: "", url: "", note_id: "", status: "confirmed", deleted: false, updated_at: "", ...extra });
const local = (day: string, time: string) => new Date(`${day}T${time}:00`).toISOString();

it("clips overnight entries at local midnight and excludes an exclusive end and trash", () => {
  const rows = [event("overnight", local("2026-10-28", "23:00"), local("2026-10-29", "01:00")), event("midnight-end", local("2026-10-28", "22:00"), local("2026-10-29", "00:00")), event("trash", local("2026-10-29", "09:00"), local("2026-10-29", "10:00"), { deleted: true }), event("all-day", "2026-10-29", "2026-10-30", { all_day: true })];
  expect(timedSegments(rows, "2026-10-28").find((row) => row.event.id === "overnight")).toMatchObject({ start: 1380, end: 1440 });
  expect(timedSegments(rows, "2026-10-29")).toMatchObject([{ event: { id: "overnight" }, start: 0, end: 60 }]);
});

it("packs a connected overlap group into independent columns and reuses space after it", () => {
  const rows = [["a", "09:00", "11:00"], ["b", "09:30", "10:30"], ["c", "10:00", "12:00"], ["d", "11:00", "12:00"], ["e", "12:00", "13:00"]].map(([id, start, end]) => event(id, local("2026-10-29", start), local("2026-10-29", end)));
  const segments = timedSegments(rows, "2026-10-29");
  expect(segments.map(({ column, columns }) => [column, columns])).toEqual([[0, 3], [1, 3], [2, 3], [0, 3], [0, 1]]);
  expect(timedSegments([...rows].reverse(), "2026-10-29")).toEqual(segments);
});

it("keeps short adjacent entries' minimum hit targets from covering each other", () => {
  const rows = [event("a", local("2026-10-29", "09:00"), local("2026-10-29", "09:01")), event("b", local("2026-10-29", "09:01"), local("2026-10-29", "09:02"))];
  expect(timedSegments(rows, "2026-10-29").map(({ column, columns }) => [column, columns])).toEqual([[0, 2], [1, 2]]);
});

it("preserves elapsed time and offset labels across real local DST boundaries", () => {
  const zone = Intl.DateTimeFormat().resolvedOptions().timeZone;
  if (zone === "America/New_York") {
    expect(localDayRange("2026-03-08").minutes).toBe(1380);
    expect(localDayRange("2026-11-01").minutes).toBe(1500);
    const rows = [event("first", "2026-11-01T01:15:00-04:00", "2026-11-01T01:45:00-04:00"), event("second", "2026-11-01T01:15:00-05:00", "2026-11-01T01:45:00-05:00")];
    expect(timedSegments(rows, "2026-11-01").map(({ start, end, column }) => [start, end, column])).toEqual([[75, 105, 0], [135, 165, 0]]);
    expect(gridClock(Date.parse(rows[0].start), true)).toBe("01:15 UTC−04:00");
    expect(gridClock(Date.parse(rows[1].start), true)).toBe("01:15 UTC−05:00");
    expect(timedSegments([event("spring", "2026-03-08T01:30:00-05:00", "2026-03-08T03:30:00-04:00")], "2026-03-08")[0]).toMatchObject({ start: 90, end: 150 });
  } else if (zone === "Pacific/Apia") {
    expect(localDayRange("2011-12-30").minutes).toBe(0);
    expect(timedSegments([event("next-day", "2011-12-31T12:00:00+14:00", "2011-12-31T13:00:00+14:00")], "2011-12-30")).toEqual([]);
  } else {
    expect(localDayRange("2026-10-29").minutes).toBe(1440);
  }
});

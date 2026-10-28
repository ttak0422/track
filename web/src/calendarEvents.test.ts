import { afterEach, describe, expect, it, vi } from "vitest";
import { calendarEventOrder, eventLocalParts, eventOnDay, eventTimeLabel, saveCalendarEvent, type CalendarEvent } from "./calendarEvents";

export const sampleEvent: CalendarEvent = {
  version: 1, id: "123e4567-e89b-42d3-a456-426614174000", revision: 2, title: "Review", all_day: true,
  start: "2026-10-28", end: "2026-10-31", time_zone: "", description: "Keep the source", location: "Desk", url: "", note_id: "", status: "confirmed", deleted: false, updated_at: "2026-10-28T10:00:00Z",
};

afterEach(() => { vi.unstubAllGlobals(); vi.useRealTimers(); });

describe("calendar events", () => {
  it("keeps multi-day all-day dates inclusive of start and exclusive of end", () => {
    expect(["2026-10-27", "2026-10-28", "2026-10-30", "2026-10-31"].map((day) => eventOnDay(sampleEvent, day))).toEqual([false, true, true, false]);
    expect(eventOnDay({ ...sampleEvent, deleted: true }, "2026-10-28")).toBe(false);
    expect(eventTimeLabel(sampleEvent)).toBe("All day · 2026-10-28 – 2026-10-30");
    expect(eventTimeLabel({ ...sampleEvent, start: "0001-01-01", end: "0001-01-02" })).toBe("All day · 0001-01-01");
  });

  it("uses local midnight boundaries for timed events even across 23/25-hour days", () => {
    for (const [month, day] of [[2, 8], [10, 1]]) {
      const event = { ...sampleEvent, all_day: false, start: new Date(2026, month, day).toISOString(), end: new Date(2026, month, day + 1).toISOString() };
      const date = `2026-${String(month + 1).padStart(2, "0")}-${String(day).padStart(2, "0")}`;
      const next = `2026-${String(month + 1).padStart(2, "0")}-${String(day + 1).padStart(2, "0")}`;
      expect(eventOnDay(event, date)).toBe(true);
      expect(eventOnDay(event, next)).toBe(false);
    }
  });

  it("labels all-day ranges independently of locally skipped civil dates", () => {
    expect(eventTimeLabel({ ...sampleEvent, start: "2011-12-29", end: "2011-12-31" })).toBe("All day · 2011-12-29 – 2011-12-30");
  });

  it("formats the event's named zone instead of the device zone", () => {
    expect(eventLocalParts("2026-10-28T15:00:45Z", "Asia/Tokyo")).toEqual({ date: "2026-10-29", time: "00:00:45" });
  });

  it("writes to the selected vault and strips the related note's qualified id", async () => {
    const fetch = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => new Response(JSON.stringify({ vault: "work", event: { ...sampleEvent, note_id: "" } }), { status: 200 }));
    vi.stubGlobal("fetch", fetch);
    const result = await saveCalendarEvent("work", sampleEvent.id, { ...sampleEvent, note_id: "work~123", expected_revision: 2, operation_id: "123e4567-e89b-42d3-a456-426614174001" });
    expect(fetch.mock.calls[0][0]).toContain("vault=work");
    const init = (fetch.mock.calls[0] as unknown as [string, RequestInit])[1];
    expect(JSON.parse(String(init.body)).note_id).toBe("123");
    expect(JSON.parse(String(init.body))).not.toHaveProperty("revision");
    expect(result.event.note_id).toBe("");
  });

  it("bounds an uncertain save response without claiming that no write happened", async () => {
    vi.useFakeTimers();
    vi.stubGlobal("fetch", (_input: RequestInfo | URL, init: RequestInit) => new Promise((_resolve, reject) => {
      init.signal?.addEventListener("abort", () => reject(new Error("Aborted")));
    }));
    const saving = saveCalendarEvent("", sampleEvent.id, { ...sampleEvent, expected_revision: 2, operation_id: "123e4567-e89b-42d3-a456-426614174001" });
    const rejected = expect(saving).rejects.toThrow("save may have completed; retry unchanged");
    await vi.advanceTimersByTimeAsync(30_000);
    await rejected;
  });

  it("rejects a qualified related note from a foreign vault before sending a request", async () => {
    const fetch = vi.fn();
    vi.stubGlobal("fetch", fetch);
    await expect(saveCalendarEvent("work", sampleEvent.id, { ...sampleEvent, note_id: "home~123", expected_revision: 2, operation_id: "123e4567-e89b-42d3-a456-426614174001" })).rejects.toThrow("related note");
    expect(fetch).not.toHaveBeenCalled();
  });

  it("orders timed rows by instant when their offsets differ", () => {
    const first = { ...sampleEvent, all_day: false, start: "2026-10-28T15:00:00+09:00" };
    const second = { ...first, start: "2026-10-28T08:00:00+00:00" };
    expect([second, first].sort(calendarEventOrder)).toEqual([first, second]);
  });
});

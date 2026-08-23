import { describe, expect, it } from "vitest";
import { daysUntil, dueBar } from "./calendarTasks";

describe("daysUntil", () => {
  it("counts whole days: same day 0, tomorrow 1, yesterday -1", () => {
    expect(daysUntil("2026-07-10", "2026-07-10")).toBe(0);
    expect(daysUntil("2026-07-11", "2026-07-10")).toBe(1);
    expect(daysUntil("2026-07-09", "2026-07-10")).toBe(-1);
  });

  it("crosses month and year boundaries, and a leap day", () => {
    expect(daysUntil("2026-02-01", "2026-01-31")).toBe(1);
    expect(daysUntil("2027-01-01", "2026-12-31")).toBe(1);
    expect(daysUntil("2028-02-29", "2028-02-27")).toBe(2);
  });

  it("stays whole across DST shifts, whichever zone the host jumps in", () => {
    // 2026-03-08 is the North American spring-forward night and 2026-10-25 the European one; a
    // local-day subtraction would count a 23- or 25-hour day there. Reading both keys as UTC keeps
    // the result independent of the host's timezone.
    expect(daysUntil("2026-03-09", "2026-03-07")).toBe(2);
    expect(daysUntil("2026-10-27", "2026-10-25")).toBe(2);
    expect(daysUntil("2026-11-01", "2026-10-30")).toBe(2);
  });
});

describe("dueBar", () => {
  const today = "2026-07-05";

  it("draws nothing for a task with only a scheduled date", () => {
    expect(dueBar(undefined, today)).toEqual({ kind: "none", fillPct: 0 });
  });

  it("fills the whole track as overdue once the deadline has passed", () => {
    expect(dueBar("2026-07-04", today)).toEqual({ kind: "overdue", fillPct: 100 });
  });

  it("fills the whole track when the deadline is today", () => {
    expect(dueBar("2026-07-05", today)).toEqual({ kind: "mark", fillPct: 100 });
  });

  it("scales the fill linearly over the window and empties it at the edge", () => {
    expect(dueBar("2026-07-12", today)).toEqual({ kind: "mark", fillPct: 50 }); // 7 of 14 days left
    expect(dueBar("2026-07-19", today).fillPct).toBe(0); // the window's edge
    expect(dueBar("2026-07-26", today).fillPct).toBe(0); // clamped past it
  });
});

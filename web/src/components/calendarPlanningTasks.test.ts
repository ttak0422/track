import { describe, expect, it } from "vitest";
import type { TaskRow } from "../types";
import { agendaTasks, calendarWeek } from "./calendarPlanningTasks";

const task = (line: number, extra: Partial<TaskRow> = {}): TaskRow => ({
  note_id: "plan", line, title: "Plan", file_kind: "note", text: `Task ${line}`, state: "TODO", done: false, ...extra,
});

describe("calendar agenda", () => {
  it("aligns Sunday weeks across years, leap days and DST boundaries", () => {
    expect(calendarWeek("2027-01-01")).toEqual([
      "2026-12-27", "2026-12-28", "2026-12-29", "2026-12-30", "2026-12-31", "2027-01-01", "2027-01-02",
    ]);
    expect(calendarWeek("2024-02-29")[4]).toBe("2024-02-29");
    expect(calendarWeek("2026-03-08")).toEqual([
      "2026-03-08", "2026-03-09", "2026-03-10", "2026-03-11", "2026-03-12", "2026-03-13", "2026-03-14",
    ]);
    expect(calendarWeek("2026-11-01")[6]).toBe("2026-11-07");
  });

  it("separates overdue deadlines, keeps WAITING, excludes terminal and undated work", () => {
    const rows = [
      task(1, { scheduled: "2026-12-31", due: "2026-12-31" }),
      task(2, { due: "2026-12-29", state: "WAITING" }),
      task(3, { scheduled: "2026-12-31", state: "DONE", done: true }),
      task(4, { due: "2026-12-31", state: "CANCELLED", done: true }),
      task(5),
      task(6, { scheduled: "2027-01-01", due: "2027-01-02" }),
    ];
    const today = agendaTasks(rows, ["2026-12-31"], "2026-12-31");
    expect(today.days[0].tasks.map((t) => t.line)).toEqual([1]);
    expect(today.overdue.map((t) => t.line)).toEqual([2]);
    const tomorrow = agendaTasks(rows, ["2027-01-01"], "2026-12-31");
    expect(tomorrow.overdue).toEqual([]);
    expect(tomorrow.days[0].tasks.map((t) => t.line)).toEqual([6]);
    expect(rows[0].scheduled).toBe("2026-12-31");
  });

  it("retains scheduled and due occurrences, sorting deadline then priority", () => {
    const result = agendaTasks([
      task(1, { scheduled: "2026-10-28", due: "2026-10-29" }),
      task(2, { due: "2026-10-28", priority: "B" }),
      task(3, { due: "2026-10-28", priority: "A" }),
    ], ["2026-10-28", "2026-10-29"], "2026-10-28");
    expect(result.days.map((day) => day.tasks.map((t) => t.line))).toEqual([[3, 2, 1], [1]]);
  });
});

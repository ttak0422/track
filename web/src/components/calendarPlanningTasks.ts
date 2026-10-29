import type { TaskRow } from "../types";
import { shiftCivilDay } from "../calendarDates";

export function calendarWeek(day: string, weekStartsOn: 0 | 1 = 0): string[] {
  const weekday = new Date(`${day}T12:00:00Z`).getUTCDay();
  const first = shiftCivilDay(day, -((weekday - weekStartsOn + 7) % 7));
  return Array.from({ length: 7 }, (_, offset) => shiftCivilDay(first, offset));
}

// A task may occur on both its working day and deadline, once per day. Overdue is a reminder
// about an unfinished deadline, never a new scheduled date.
export function agendaTasks(tasks: TaskRow[], days: string[], today: string) {
  const open = tasks.filter((task) => !task.done && task.state !== "DONE" && task.state !== "CANCELLED");
  return {
    days: days.map((day) => ({
      day,
      tasks: open.filter((task) => task.scheduled === day || task.due === day).sort(compareTasks),
    })),
    overdue: days.includes(today)
      ? open.filter((task) => task.due && task.due < today).sort(compareTasks)
      : [],
  };
}

function compareTasks(a: TaskRow, b: TaskRow): number {
  return (a.due ?? "9999").localeCompare(b.due ?? "9999") ||
    (a.priority ?? "~").localeCompare(b.priority ?? "~") ||
    a.note_id.localeCompare(b.note_id) || a.line - b.line;
}

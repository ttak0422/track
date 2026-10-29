import type { TaskRow } from "../types";
import { shiftDay } from "./activityDates";

export function calendarWeek(day: string): string[] {
  const first = shiftDay(day, -new Date(`${day}T12:00:00`).getDay());
  return Array.from({ length: 7 }, (_, offset) => shiftDay(first, offset));
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

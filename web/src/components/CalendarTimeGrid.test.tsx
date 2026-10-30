import { fireEvent, render, within } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import type { CalendarEvent } from "../calendarEvents";
import { CalendarTimeGrid } from "./CalendarTimeGrid";
const event = (id: string, all_day: boolean, start: string, end: string, extra: Partial<CalendarEvent> = {}): CalendarEvent => ({ version: 1, id, revision: 1, title: id, all_day, start, end, time_zone: "UTC", description: "", location: "", url: "", note_id: "", status: "confirmed", deleted: false, updated_at: "", ...extra });
const instant = (clock: string) => new Date(`2026-10-29T${clock}:00`).toISOString();

it("separates civil all-day ranges, keeps empty dates, and edits the same stable event", () => {
  const edit = vi.fn();
  const allDay = event("Three days", true, "2026-10-28", "2026-10-31");
  const timed = event("Meeting", false, instant("09:00"), instant("10:00"), { status: "cancelled" });
  const { getByRole, getByText, queryByRole } = render(<CalendarTimeGrid days={["2026-10-29", "2026-10-30", "2026-10-31"]} events={[allDay, timed]} onEdit={edit} />);
  expect(within(getByRole("region", { name: "All-day events on 2026-10-29" })).getByRole("button", { name: /Edit Three days/ })).toBeVisible();
  expect(within(getByRole("region", { name: "All-day events on 2026-10-30" })).getByRole("button", { name: /Edit Three days/ })).toBeVisible();
  expect(within(getByRole("region", { name: "All-day events on 2026-10-31" })).queryByRole("button")).toBeNull();
  expect(getByText("Cancelled · Meeting")).toBeVisible();
  expect(getByText("09:00–10:00")).toBeVisible();
  fireEvent.click(getByRole("button", { name: /Edit Meeting on 2026-10-29/ }));
  expect(edit).toHaveBeenCalledWith(timed);
  expect(queryByRole("button", { name: /Edit Meeting on 2026-10-30/ })).toBeNull();
  expect(getByRole("region", { name: "Event time grid, scroll for more days and hours" })).toHaveAttribute("tabindex", "0");
});

it("announces cancellation in both timed and all-day event button names", () => {
  const {getByRole} = render(<CalendarTimeGrid days={["2026-10-29"]} events={[
    event("Workshop", true, "2026-10-29", "2026-10-30", { status: "cancelled" }),
    event("Call", false, instant("09:00"), instant("10:00"), { status: "cancelled" }),
  ]} onEdit={vi.fn()} />);
  expect(getByRole("button", {name: /^Edit Workshop on 2026-10-29: Cancelled · All day/})).toBeVisible();
  expect(getByRole("button", {name: /^Edit Call on 2026-10-29: Cancelled · /})).toBeVisible();
});

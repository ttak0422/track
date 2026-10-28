import { fireEvent, render, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { CalendarEventsPanel } from "./CalendarEventsPanel";
import type { CalendarEvent } from "../calendarEvents";

const state = vi.hoisted(() => ({ rows: [] as CalendarEvent[], static: false }));
const save = vi.hoisted(() => ({ mutateAsync: vi.fn(), isPending: false }));
const dialog = vi.hoisted(() => vi.fn());
vi.mock("../calendarEventQueries", () => ({ useCalendarEventsQuery: () => ({ data: { events: state.rows } }), useSaveCalendarEvent: () => save }));
vi.mock("../runtime", () => ({ get STATIC_MODE() { return state.static; } }));
vi.mock("../vaultScope", () => ({ useVaultScope: () => ({ scope: "work", activeName: "home" }) }));
vi.mock("../queries", () => ({ useNotesQuery: () => ({ data: { notes: [
  { note_id: "work~1", title: "Work source" }, { note_id: "home~1", title: "Private source" },
] } }) }));
vi.mock("./CalendarEventDialog", () => ({ CalendarEventDialog: (props: unknown) => { dialog(props); return <span>Editor</span>; } }));
vi.mock("@tanstack/react-router", () => ({ Link: ({ children }: { children: React.ReactNode }) => <a>{children}</a> }));
const event: CalendarEvent = { version: 1, id: "123e4567-e89b-42d3-a456-426614174000", revision: 5, title: "Deleted review", all_day: true, start: "2026-10-28", end: "2026-10-29", time_zone: "", description: "Source", location: "Desk", url: "", note_id: "", status: "cancelled", deleted: true, updated_at: "2026-10-28T10:00:00Z" };

beforeEach(() => { state.rows = [event]; state.static = false; save.mutateAsync.mockReset().mockResolvedValue({ event: { ...event, revision: 6, deleted: false } }); dialog.mockClear(); });

it("keeps trash out of the agenda, then restores with the latest revision", async () => {
  const { getByRole, queryByText } = render(<CalendarEventsPanel days={["2026-10-28"]} defaultDate="2026-10-28" />);
  expect(queryByText("Deleted review")).toBeNull();
  fireEvent.click(getByRole("button", { name: "Show trash" }));
  fireEvent.click(getByRole("button", { name: "Restore Deleted review" }));
  await waitFor(() => expect(save.mutateAsync).toHaveBeenCalledTimes(1));
  expect(save.mutateAsync.mock.calls[0][0]).toMatchObject({ id: event.id, request: { deleted: false, title: "Deleted review", expected_revision: 5 } });
});

it("limits related note selection to the event's selected vault", () => {
  const { getByRole } = render(<CalendarEventsPanel days={["2026-10-28"]} defaultDate="2026-10-28" />);
  fireEvent.click(getByRole("button", { name: "New event" }));
  expect(dialog.mock.calls.at(-1)?.[0]).toMatchObject({ vault: "work", notes: [{ note_id: "work~1", title: "Work source" }] });
});

it("does not mount event data or registration controls on a published site", () => {
  state.static = true;
  const { container } = render(<CalendarEventsPanel days={["2026-10-28"]} defaultDate="2026-10-28" />);
  expect(container).toBeEmptyDOMElement();
});

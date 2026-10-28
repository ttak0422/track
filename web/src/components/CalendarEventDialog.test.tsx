import { act, fireEvent, render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { CalendarEventDialog } from "./CalendarEventDialog";
import type { CalendarEvent } from "../calendarEvents";

const mutation = vi.hoisted(() => ({ mutateAsync: vi.fn(), isPending: false }));
vi.mock("../calendarEventQueries", () => ({ useSaveCalendarEvent: () => mutation }));
const event: CalendarEvent = { version: 1, id: "123e4567-e89b-42d3-a456-426614174000", revision: 4, title: "Review", all_day: true, start: "2026-10-28", end: "2026-10-31", time_zone: "", description: "Source", location: "Desk", url: "", note_id: "", status: "confirmed", deleted: false, updated_at: "2026-10-28T10:00:00Z" };

beforeEach(() => { mutation.mutateAsync.mockReset(); mutation.isPending = false; });

describe("event editor", () => {
  it("preserves all-day dates on title-only edits across a locally skipped day", async () => {
    const skipped = { ...event, start: "2011-12-29", end: "2011-12-31" };
    mutation.mutateAsync.mockResolvedValue({ event: skipped });
    const onSaved = vi.fn();
    const { getByLabelText, getByRole } = render(<CalendarEventDialog vault="" date="2011-12-29" event={skipped} notes={[]} onSaved={onSaved} onClose={() => {}} />);
    expect((getByLabelText("Last day") as HTMLInputElement).value).toBe("2011-12-30");
    fireEvent.change(getByLabelText("Title"), { target: { value: "Revised title" } });
    fireEvent.click(getByRole("button", { name: "Save event" }));
    await waitFor(() => expect(onSaved).toHaveBeenCalled());
    expect(mutation.mutateAsync.mock.calls[0][0].request).toMatchObject({ start: skipped.start, end: skipped.end });
  });
  it("converts an inclusive last day to an exclusive date and blocks double submission", async () => {
    let resolve!: (value: unknown) => void;
    mutation.mutateAsync.mockImplementation(() => new Promise((done) => { resolve = done; }));
    const onSaved = vi.fn();
    const onClose = vi.fn();
    const { getByLabelText, getByRole } = render(<CalendarEventDialog vault="work" date="2026-10-28" notes={[]} onSaved={onSaved} onClose={onClose} />);
    fireEvent.change(getByLabelText("Title"), { target: { value: "Three days" } });
    fireEvent.input(getByLabelText("Last day"), { target: { value: "2026-10-30" } });
    fireEvent.change(getByLabelText("Description"), { target: { value: "After date edit" } });
    fireEvent.click(getByRole("button", { name: "Save event" }));
    fireEvent.click(getByRole("button", { name: "Save event" }));
    fireEvent.click(getByRole("button", { name: "Cancel" }));
    expect(mutation.mutateAsync).toHaveBeenCalledTimes(1);
    expect(onClose).not.toHaveBeenCalled();
    expect(mutation.mutateAsync.mock.calls[0][0].request).toMatchObject({ title: "Three days", start: "2026-10-28", end: "2026-10-31", all_day: true, expected_revision: 0 });
    await act(async () => resolve({ event }));
    expect(onSaved).toHaveBeenCalledWith(event);
  });

  it("retains failed inputs and reuses the same event and operation for an unchanged retry", async () => {
    mutation.mutateAsync.mockRejectedValueOnce(new Error("Network response lost")).mockResolvedValueOnce({ event });
    const onSaved = vi.fn();
    const { getByLabelText, getByRole, findByRole } = render(<CalendarEventDialog vault="" date="2026-10-28" notes={[]} onSaved={onSaved} onClose={() => {}} />);
    fireEvent.change(getByLabelText("Title"), { target: { value: "Keep me" } });
    fireEvent.click(getByRole("button", { name: "Save event" }));
    expect((await findByRole("alert")).textContent).toContain("Network response lost");
    expect((getByLabelText("Title") as HTMLInputElement).value).toBe("Keep me");
    fireEvent.click(getByRole("button", { name: "Save event" }));
    await waitFor(() => expect(onSaved).toHaveBeenCalled());
    expect(mutation.mutateAsync.mock.calls[1][0]).toEqual(mutation.mutateAsync.mock.calls[0][0]);
  });

  it("preserves stored fields when deleting an event despite unsaved edits", async () => {
    mutation.mutateAsync.mockResolvedValue({ event: { ...event, deleted: true, revision: 5 } });
    const onSaved = vi.fn();
    const { getByLabelText, getByRole } = render(<CalendarEventDialog vault="" date="2026-10-28" event={event} notes={[]} onSaved={onSaved} onClose={() => {}} />);
    fireEvent.change(getByLabelText("Title"), { target: { value: "Unsaved title" } });
    fireEvent.click(getByRole("button", { name: "Move to trash" }));
    await waitFor(() => expect(onSaved).toHaveBeenCalled());
    expect(mutation.mutateAsync.mock.calls[0][0].request).toMatchObject({ title: "Review", description: "Source", deleted: true, expected_revision: 4 });
    expect(mutation.mutateAsync.mock.calls[0][0].request).not.toHaveProperty("version");
  });

  it("keeps timed zone input and exposes explicit offsets for an ambiguous clock time", async () => {
    mutation.mutateAsync.mockRejectedValue(new Error("start is ambiguous; include an explicit UTC offset"));
    const { getByLabelText, getByRole, findByRole } = render(<CalendarEventDialog vault="" date="2026-11-01" notes={[]} onSaved={() => {}} onClose={() => {}} />);
    fireEvent.change(getByLabelText("Title"), { target: { value: "Repeated hour" } });
    fireEvent.click(getByLabelText("All day"));
    fireEvent.change(getByLabelText("Time zone"), { target: { value: "America/New_York" } });
    fireEvent.input(getByLabelText("Start time"), { target: { value: "01:30" } });
    fireEvent.click(getByRole("button", { name: "Save event" }));
    await findByRole("alert");
    expect((getByLabelText("Time zone") as HTMLInputElement).value).toBe("America/New_York");
    expect(getByLabelText("Start UTC offset").closest("details")?.open).toBe(true);
  });

  it("cancels without writing and returns focus to the invoking control", () => {
    const onClose = vi.fn();
    const trigger = document.createElement("button");
    document.body.append(trigger);
    trigger.focus();
    const { getByRole, unmount } = render(<CalendarEventDialog vault="" date="2026-10-28" notes={[]} onSaved={() => {}} onClose={onClose} />);
    fireEvent(getByRole("dialog"), new Event("cancel", { bubbles: true, cancelable: true }));
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(mutation.mutateAsync).not.toHaveBeenCalled();
    unmount();
    expect(document.activeElement).toBe(trigger);
    trigger.remove();
  });

  it("preserves a repeated local time's chosen offsets and seconds on re-edit", async () => {
    const timed = { ...event, all_day: false, start: "2026-11-01T01:30:45.123-04:00", end: "2026-11-01T01:30:45.456-05:00", time_zone: "America/New_York" };
    mutation.mutateAsync.mockResolvedValue({ event: { ...timed, revision: 5 } });
    const onSaved = vi.fn();
    const { getByLabelText, getByRole } = render(<CalendarEventDialog vault="" date="2026-11-01" event={timed} notes={[]} onSaved={onSaved} onClose={() => {}} />);
    fireEvent.change(getByLabelText("Title"), { target: { value: "Revised title" } });
    fireEvent.click(getByRole("button", { name: "Save event" }));
    await waitFor(() => expect(onSaved).toHaveBeenCalled());
    expect(mutation.mutateAsync.mock.calls[0][0].request).toMatchObject({ start: timed.start, end: timed.end, time_zone: timed.time_zone, expected_revision: 4 });
  });
});

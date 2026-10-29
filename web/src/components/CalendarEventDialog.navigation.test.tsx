import { createBrowserHistory, createRootRoute, createRoute, createRouter, Outlet, RouterProvider } from "@tanstack/react-router";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { StrictMode, useState } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { CalendarEvent } from "../calendarEvents";
import { CalendarEventDialog } from "./CalendarEventDialog";

const mutation = vi.hoisted(() => ({ mutateAsync: vi.fn(), isPending: false }));
vi.mock("../calendarEventQueries", () => ({ useSaveCalendarEvent: () => mutation }));
const saved: CalendarEvent = { version: 1, id: "123e4567-e89b-42d3-a456-426614174000", revision: 1, title: "Saved fixture", all_day: true, start: "2026-10-28", end: "2026-10-29", time_zone: "", description: "", location: "", url: "", note_id: "", status: "confirmed", deleted: false, updated_at: "2026-10-28T12:00:00Z" };
let existing: CalendarEvent | undefined;
const dispose: (() => void)[] = [];

function EditorPage({ date = "2026-10-28" }) {
  const [open, setOpen] = useState(false);
  return <><h1>Event page {date}</h1><button onClick={() => setOpen(true)}>Open event editor</button>
    {open ? <CalendarEventDialog vault="" date={date} event={existing} notes={[]} onSaved={() => setOpen(false)} onClose={() => setOpen(false)} /> : null}</>;
}

async function setup(path: "/calendar" | "/day/$date" = "/calendar") {
  window.history.replaceState({}, "", "/tasks");
  const history = createBrowserHistory();
  const root = createRootRoute({ component: Outlet });
  const tasks = createRoute({ getParentRoute: () => root, path: "/tasks", component: () => <h1>Unrelated tasks</h1> });
  const calendar = createRoute({ getParentRoute: () => root, path: "/calendar", component: EditorPage });
  const day = createRoute({ getParentRoute: () => root, path: "/day/$date", component: () => <EditorPage date={day.useParams().date} /> });
  const router = createRouter({ history, routeTree: root.addChildren([tasks, calendar, day]), defaultPendingMinMs: 0 });
  const view = render(<StrictMode><RouterProvider router={router} /></StrictMode>);
  dispose.push(() => { view.unmount(); history.destroy(); });
  await screen.findByRole("heading", { name: "Unrelated tasks" });
  await act(async () => { await router.navigate({ to: path, params: { date: "2026-10-28" } }); });
  fireEvent.click(await screen.findByRole("button", { name: "Open event editor" }));
  await screen.findByRole("dialog");
  return { history, router };
}

function unloadBlocked() {
  const event = new Event("beforeunload", { cancelable: true });
  window.dispatchEvent(event);
  return event.defaultPrevented;
}

beforeEach(() => {
  mutation.mutateAsync.mockReset(); mutation.isPending = false; existing = undefined;
  vi.spyOn(window, "scrollTo").mockImplementation(() => {});
});
afterEach(() => { dispose.splice(0).forEach((fn) => fn()); vi.restoreAllMocks(); window.history.replaceState({}, "", "/"); });

describe("event drafts through browser history and beforeunload", () => {
  it("leaves a clean form without confirmation and does not block unrelated routes afterward", async () => {
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
    const { history, router } = await setup();
    expect(unloadBlocked()).toBe(false);
    await act(async () => history.back());
    await screen.findByRole("heading", { name: "Unrelated tasks" });
    expect(screen.queryByRole("dialog")).toBeNull();
    await act(async () => { await router.navigate({ to: "/calendar" }); });
    expect(unloadBlocked()).toBe(false);
    expect(confirm).not.toHaveBeenCalled();
  });

  it("keeps inputs after cancelled browser Back, then confirms accepted Back only once", async () => {
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
    const { history } = await setup();
    fireEvent.change(screen.getByLabelText("Title"), { target: { value: "Keep this event" } });
    fireEvent.change(screen.getByLabelText("Description"), { target: { value: "Unsaved details" } });
    await act(async () => history.back());
    await waitFor(() => expect(confirm).toHaveBeenCalledOnce());
    await waitFor(() => expect(window.location.pathname).toBe("/calendar"));
    expect(screen.getByLabelText("Title")).toHaveValue("Keep this event");
    expect(screen.getByLabelText("Description")).toHaveValue("Unsaved details");
    expect(unloadBlocked()).toBe(true);
    confirm.mockReturnValue(true);
    await act(async () => history.back());
    await screen.findByRole("heading", { name: "Unrelated tasks" });
    expect(confirm).toHaveBeenCalledTimes(2);
    expect(unloadBlocked()).toBe(false);
  });

  it("reads live DOM input and composition before React commits and clears reverted edits", async () => {
    await setup();
    const title = screen.getByLabelText("Title") as HTMLInputElement;
    title.value = "DOM input before commit";
    expect(unloadBlocked()).toBe(true);
    title.value = "";
    expect(unloadBlocked()).toBe(false);
    fireEvent.compositionStart(title);
    expect(unloadBlocked()).toBe(true);
    fireEvent.compositionEnd(title);
    expect(unloadBlocked()).toBe(false);
  });

  it("protects pending and failed saves, and releases after intentional cancellation", async () => {
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
    let reject!: (reason: Error) => void;
    mutation.mutateAsync.mockImplementation(() => new Promise((_, fail) => { reject = fail; }));
    const { history } = await setup();
    fireEvent.change(screen.getByLabelText("Title"), { target: { value: "Pending draft" } });
    fireEvent.click(screen.getByRole("button", { name: "Save event" }));
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(unloadBlocked()).toBe(true);
    await act(async () => history.back());
    await waitFor(() => expect(confirm).toHaveBeenCalledOnce());
    await waitFor(() => expect(window.location.pathname).toBe("/calendar"));
    await act(async () => reject(new Error("Fixture save failed")));
    expect(await screen.findByRole("alert")).toHaveTextContent("Fixture save failed");
    expect(unloadBlocked()).toBe(true);
    await act(async () => history.back());
    await waitFor(() => expect(confirm).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(window.location.pathname).toBe("/calendar"));
    expect(screen.getByLabelText("Title")).toHaveValue("Pending draft");
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(unloadBlocked()).toBe(false);
    await act(async () => history.back());
    await screen.findByRole("heading", { name: "Unrelated tasks" });
    expect(confirm).toHaveBeenCalledTimes(2);
  });

  it("protects a failed unchanged existing-event save and releases after confirmed success", async () => {
    existing = saved;
    mutation.mutateAsync.mockRejectedValueOnce(new Error("Lost response")).mockResolvedValueOnce({ event: saved });
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
    const { history } = await setup();
    expect(unloadBlocked()).toBe(false);
    fireEvent.click(screen.getByRole("button", { name: "Save event" }));
    await screen.findByRole("alert");
    expect(unloadBlocked()).toBe(true);
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Save event" })));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(unloadBlocked()).toBe(false);
    await act(async () => history.back());
    await screen.findByRole("heading", { name: "Unrelated tasks" });
    expect(confirm).not.toHaveBeenCalled();
  });

  it.each(["Cancel", "Escape"])("releases the guard after intentional %s", async (action) => {
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
    const { history } = await setup();
    fireEvent.change(screen.getByLabelText("Description"), { target: { value: "Discard intentionally" } });
    expect(unloadBlocked()).toBe(true);
    if (action === "Cancel") fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    else fireEvent(screen.getByRole("dialog"), new Event("cancel", { bubbles: true, cancelable: true }));
    expect(unloadBlocked()).toBe(false);
    await act(async () => history.back());
    await screen.findByRole("heading", { name: "Unrelated tasks" });
    expect(confirm).not.toHaveBeenCalled();
  });

  it("guards day-parameter navigation and ignores an old pending save after accepted departure", async () => {
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
    let resolve!: (value: { event: CalendarEvent }) => void;
    mutation.mutateAsync.mockImplementation(() => new Promise((done) => { resolve = done; }));
    const { router } = await setup("/day/$date");
    fireEvent.change(screen.getByLabelText("Title"), { target: { value: "Older pending event" } });
    fireEvent.click(screen.getByRole("button", { name: "Save event" }));
    await act(async () => { void router.navigate({ to: "/day/$date", params: { date: "2026-10-29" } }); });
    expect(confirm).toHaveBeenCalledOnce();
    expect(screen.getByLabelText("Title")).toHaveValue("Older pending event");
    confirm.mockReturnValue(true);
    await act(async () => { await router.navigate({ to: "/day/$date", params: { date: "2026-10-29" } }); });
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(confirm).toHaveBeenCalledTimes(2);
    fireEvent.click(screen.getByRole("button", { name: "Open event editor" }));
    fireEvent.change(screen.getByLabelText("Title"), { target: { value: "Newer draft" } });
    await act(async () => resolve({ event: saved }));
    expect(screen.getByLabelText("Title")).toHaveValue("Newer draft");
    expect(unloadBlocked()).toBe(true);
  });
});

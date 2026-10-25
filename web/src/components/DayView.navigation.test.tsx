import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, RouterProvider } from "@tanstack/react-router";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { getAgenda, openJournal } from "../api";
import type { AgendaResponse } from "../types";
import { DayView } from "./DayView";

const mode = vi.hoisted(() => ({ static: false }));
vi.mock("../runtime", () => ({ get STATIC_MODE() { return mode.static; } }));
vi.mock("../api", async (original) => ({
  ...(await original<typeof import("../api")>()),
  getAgenda: vi.fn(),
  openJournal: vi.fn(),
  resolveTerm: vi.fn(async () => ({ found: false })),
  listDatedTasks: vi.fn(async () => ({ tasks: [
    { note_id: "1", title: "Plan", line: 1, text: "Year end task", due: "2026-12-31" },
    { note_id: "1", title: "Plan", line: 2, text: "New year task", scheduled: "2027-01-01" },
  ] })),
}));

function mountDay(date: string) {
  const root = createRootRoute();
  const day = createRoute({ getParentRoute: () => root, path: "/day/$date", component: () => {
    const { date } = day.useParams();
    return <DayView date={date} />;
  } });
  const history = createMemoryHistory({ initialEntries: [`/day/${date}`] });
  const router = createRouter({ routeTree: root.addChildren([day]), history, defaultPendingMinMs: 0 });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const view = render(<QueryClientProvider client={client}><RouterProvider router={router} /></QueryClientProvider>);
  disposals.push(() => { view.unmount(); history.destroy(); client.clear(); });
  return router;
}

const disposals: (() => void)[] = [];
afterEach(() => {
  disposals.splice(0).forEach((dispose) => dispose());
  vi.restoreAllMocks();
});
beforeEach(() => {
  vi.spyOn(window, "scrollTo").mockImplementation(() => {});
  vi.mocked(openJournal).mockClear();
  vi.mocked(getAgenda).mockReset().mockImplementation(async (date) => ({ date, notes: [] }));
});

describe.each([false, true])("day navigation (static=%s)", (staticMode) => {
  beforeEach(() => { mode.static = staticMode; });

  it("updates the route, heading and tasks on consecutive clicks, and keeps button focus", async () => {
    const router = mountDay("2026-12-31");
    const next = await screen.findByRole("button", { name: "Next day" });
    await screen.findByText("Year end task");
    next.focus();
    fireEvent.click(next);
    await screen.findByRole("heading", { name: "2027-01-01" });
    expect(router.state.location.pathname).toBe("/day/2027-01-01");
    expect(screen.queryByText("Year end task")).toBeNull();
    expect(screen.getByText("New year task")).toBeTruthy();
    expect(document.activeElement).toBe(next);
    fireEvent.click(next);
    await screen.findByRole("heading", { name: "2027-01-02" });
    expect(screen.queryByText("New year task")).toBeNull();
    await screen.findByText("No notes were worked on this day.");
    fireEvent.click(screen.getByRole("button", { name: "Previous day" }));
    await screen.findByRole("heading", { name: "2027-01-01" });
    expect(openJournal).not.toHaveBeenCalled();
  });

  it("keeps the newest day's content when an earlier response arrives last", async () => {
    const pending = new Map<string, (data: AgendaResponse) => void>();
    vi.mocked(getAgenda).mockImplementation((date) => new Promise((resolve) => pending.set(date, resolve)));
    mountDay("2024-02-28");
    const next = await screen.findByRole("button", { name: "Next day" });
    await waitFor(() => expect(pending.has("2024-02-28")).toBe(true));
    fireEvent.click(next);
    await screen.findByRole("heading", { name: "2024-02-29" });
    await waitFor(() => expect(pending.has("2024-02-29")).toBe(true));
    fireEvent.click(next);
    await screen.findByRole("heading", { name: "2024-03-01" });
    await waitFor(() => expect(pending.has("2024-03-01")).toBe(true));
    await act(async () => pending.get("2024-03-01")!({ date: "2024-03-01", notes: [
      { note_id: "march", file_kind: "note", title: "March activity" },
    ] }));
    await screen.findByText("March activity");
    await act(async () => {
      for (const date of ["2024-02-29", "2024-02-28"]) pending.get(date)!({ date, notes: [
        { note_id: date, file_kind: "note", title: "Old activity" },
      ] });
    });
    expect(screen.getByRole("heading", { name: "2024-03-01" })).toBeTruthy();
    expect(screen.getByText("March activity")).toBeTruthy();
    expect(screen.queryByText("Old activity")).toBeNull();
    expect(openJournal).not.toHaveBeenCalled();
  });
});

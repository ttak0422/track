import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { getTaskTimeline } from "../api";
import type { TaskTimeline } from "../types";
import { TaskTimelinePanel, TimelineSnapshotView } from "./TaskTimelinePanel";

vi.mock("../api", () => ({ getTaskTimeline: vi.fn() }));

beforeEach(() => vi.clearAllMocks());

const snapshot: TaskTimeline = {
  schema_version: 1, note_id: "1", title: "Project",
  progress: { total: 2, done: 1, cancelled: 0, open: 1 },
  items: [
    { line: 4, state: "TODO", done: false, text: "Parent", depth: 0 },
    { line: 5, state: "DONE", done: true, text: "Child", depth: 1, parent_line: 4 },
  ],
  history: [
    { at: "2026-11-01T00:00:00Z", line: 4, from: "TODO", to: "DONE", text: "Former task" },
    { at: "2026-10-31T23:59:59Z", line: 5, from: "TODO", to: "DONE", text: "Child" },
  ],
};

function client() {
  return new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
}

describe("TaskTimelinePanel", () => {
  it("reads only when opened, accepts a qualified vault id, and closes without editing", async () => {
    vi.mocked(getTaskTimeline).mockResolvedValue(snapshot);
    const queryClient = client();
    render(<QueryClientProvider client={queryClient}><TaskTimelinePanel noteID="work~1" /></QueryClientProvider>);
    expect(getTaskTimeline).not.toHaveBeenCalled();
    const toggle = screen.getByRole("button", { name: "Task timeline" });
    fireEvent.click(toggle);
    expect(await screen.findByText(/Saved tasks: 1 done/)).toBeTruthy();
    expect(getTaskTimeline).toHaveBeenCalledWith("work~1", expect.any(AbortSignal));
    expect(toggle.getAttribute("aria-expanded")).toBe("true");
    fireEvent.click(toggle);
    expect(screen.queryByText(/Saved tasks:/)).toBeNull();
    queryClient.clear();
  });

  it("cancels an unfinished read when closed and can reopen", async () => {
    let signal: AbortSignal | undefined;
    vi.mocked(getTaskTimeline).mockImplementationOnce((_id, currentSignal) => {
      signal = currentSignal;
      return new Promise(() => {});
    }).mockResolvedValue(snapshot);
    const queryClient = client();
    render(<QueryClientProvider client={queryClient}><TaskTimelinePanel noteID="1" /></QueryClientProvider>);
    const toggle = screen.getByRole("button", { name: "Task timeline" });
    fireEvent.click(toggle);
    expect(screen.getByRole("status").textContent).toBe("Loading timeline...");
    fireEvent.click(toggle);
    expect(signal?.aborted).toBe(true);
    fireEvent.click(toggle);
    expect(await screen.findByText(/Saved tasks: 1 done/)).toBeTruthy();
    queryClient.clear();
  });

  it("shows read failures instead of an empty success", async () => {
    vi.mocked(getTaskTimeline).mockRejectedValue(new Error("Timeline unavailable"));
    const queryClient = client();
    render(<QueryClientProvider client={queryClient}><TaskTimelinePanel noteID="1" /></QueryClientProvider>);
    fireEvent.click(screen.getByRole("button", { name: "Task timeline" }));
    await waitFor(() => expect(screen.getByRole("alert").textContent).toBe("Timeline unavailable"));
    expect(screen.queryByText("No tasks in this note.")).toBeNull();
    queryClient.clear();
  });

  it("labels retained data as stale after a refresh fails", async () => {
    vi.mocked(getTaskTimeline).mockRejectedValue(new Error("Refresh failed"));
    const queryClient = client();
    queryClient.setQueryData(["tasks", "timeline", "1"], snapshot);
    render(<QueryClientProvider client={queryClient}><TaskTimelinePanel noteID="1" /></QueryClientProvider>);
    fireEvent.click(screen.getByRole("button", { name: "Task timeline" }));
    expect(await screen.findByText("Showing the last retrieved timeline; it may be stale.")).toBeTruthy();
    expect(screen.getByText(/Saved tasks: 1 done/)).toBeTruthy();
    queryClient.clear();
  });

  it("keeps append order and historical entries separate from current line identities", () => {
    render(<TimelineSnapshotView timeline={snapshot} />);
    const history = screen.getByRole("list", { name: "Recorded transitions" });
    expect(history.querySelectorAll("li")[0].textContent).toContain("Former task");
    expect(history.querySelectorAll("li")[1].textContent).toContain("Child");
    expect(history.querySelectorAll("a")).toHaveLength(0);
    const current = screen.getByRole("list", { name: "Current tasks" });
    expect(current.querySelectorAll("li")[1].style.paddingInlineStart).toBe("1em");
  });

  it("renders task text as text, not HTML, and explains empty history", () => {
    const data = { ...snapshot, items: [{ ...snapshot.items[0], text: "<script>alert(1)</script>" }], history: [] };
    const { container } = render(<TimelineSnapshotView timeline={data} />);
    expect(screen.getByText("<script>alert(1)</script>")).toBeTruthy();
    expect(container.querySelector("script")).toBeNull();
    expect(screen.getByText("No recorded transitions.")).toBeTruthy();
  });
});

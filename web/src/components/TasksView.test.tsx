import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { TasksView, taskMark } from "./TasksView";

const openTasks = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-router", () => ({
  // href, so the rows keep the link role the real router gives them.
  Link: ({ children }: { children?: ReactNode }) => <a href="#note">{children}</a>,
}));

vi.mock("../queries", () => ({ useOpenTasksQuery: (enabled?: boolean) => openTasks(enabled) }));

describe("taskMark", () => {
  it("labels a row by what decides its place in the order", () => {
    expect(taskMark({ priority: "A", due: "2026-08-01" })).toBe("A");
    expect(taskMark({ due: "2026-08-01" })).toBe("!");
    expect(taskMark({})).toBe("▷");
  });
});

describe("TasksView", () => {
  it("lists the open tasks in the order the engine returned them", () => {
    openTasks.mockReturnValue({
      data: {
        tasks: [
          { note_id: "1", file_kind: "note", title: "Project", line: 3, state: "TODO", done: false, priority: "A", scheduled: "2026-07-30", text: "write the thing" },
          { note_id: "2", file_kind: "note", title: "Other", line: 9, state: "DOING", done: false, due: "2026-08-01", text: "chase the other" },
        ],
      },
    });
    render(<TasksView />);

    expect(screen.getByText("2 open")).toBeTruthy();
    const rows = screen.getAllByRole("link").map((el) => el.textContent);
    expect(rows[0]).toContain("write the thing");
    expect(rows[0]).toContain("Project");
    expect(rows[1]).toContain("chase the other");
  });

  it("says so when nothing is open, rather than showing an empty page", () => {
    openTasks.mockReturnValue({ data: { tasks: [] } });
    render(<TasksView />);
    expect(screen.getByText("Nothing to do.")).toBeTruthy();
    expect(screen.queryByText(/open$/)).toBeNull();
  });
});

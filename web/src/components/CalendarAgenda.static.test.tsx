import { render } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { CalendarAgenda } from "./CalendarAgenda";

vi.mock("../runtime", () => ({ STATIC_MODE: true }));
vi.mock("@tanstack/react-router", () => ({
  Link: ({ to, params, children }: { to: string; params: Record<string, string>; children: React.ReactNode }) =>
    <a href={to.replace(/\$(\w+)/g, (_, key: string) => params[key])}>{children}</a>,
}));

it("identifies the published agenda as a read-only snapshot", () => {
  const { getByText, queryByText } = render(<CalendarAgenda tasks={[]} days={["2026-10-28"]} today="2026-10-28" />);
  expect(getByText("Read-only snapshot. Open a task’s note for details.")).toBeTruthy();
  expect(queryByText("Open a task’s note to change its state or dates.")).toBeNull();
});

it("does not link agenda dates to day pages absent from a static export", () => {
  const { container, getByRole } = render(<CalendarAgenda
    tasks={[{ note_id: "published-note", title: "Plan", file_kind: "note", line: 1, state: "TODO", done: false, text: "Review", due: "2026-10-29" }]}
    days={["2026-10-28", "2026-10-29"]} today="2026-10-28"
  />);
  expect(getByRole("heading", { name: "2026-10-28 · Today" })).toBeTruthy();
  expect(container.querySelector('a[href^="/day/"]')).toBeNull();
  expect(getByRole("link").getAttribute("href")).toBe("/notes/published-note");
});

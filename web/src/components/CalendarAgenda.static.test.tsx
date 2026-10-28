import { render } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { CalendarAgenda } from "./CalendarAgenda";

vi.mock("../runtime", () => ({ STATIC_MODE: true }));
vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: React.ReactNode }) => <a>{children}</a>,
}));

it("identifies the published agenda as a read-only snapshot", () => {
  const { getByText, queryByText } = render(<CalendarAgenda tasks={[]} days={["2026-10-28"]} today="2026-10-28" />);
  expect(getByText("Read-only snapshot. Open a task’s note for details.")).toBeTruthy();
  expect(queryByText("Open a task’s note to change its state or dates.")).toBeNull();
});

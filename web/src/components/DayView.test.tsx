import { fireEvent, render, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { DayView } from "./DayView";
vi.mock("./CalendarEventsPanel", () => ({ CalendarEventsPanel: () => <p>Day appointment fixture</p> }));

const agendaQuery = vi.hoisted(() => ({ current: {} as Record<string, unknown> }));
const navigate = vi.hoisted(() => vi.fn());
const openJournal = vi.hoisted(() => vi.fn());
const tasksQuery = vi.hoisted(() => ({ current: {} as Record<string, unknown> }));

vi.mock("../queries", () => ({
  useAgendaQuery: () => agendaQuery.current,
  useResolveQuery: () => ({ data: undefined }),
  useDatedTasksQuery: () => tasksQuery.current,
}));
vi.mock("../api", () => ({ openJournal }));
vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => navigate,
  Link: ({
    to,
    params,
    children,
    ...rest
  }: {
    to: string;
    params?: Record<string, string>;
    children: React.ReactNode;
  } & Record<string, unknown>) => (
    <a href={to.replace(/\$(\w+)/g, (_, key: string) => params?.[key] ?? "")} {...rest}>
      {children}
    </a>
  ),
}));

const loadedAgenda = {
  isPending: false,
  isError: false,
  error: null as Error | null,
  data: {
    date: "2026-07-03",
    notes: [
      { note_id: "a", file_kind: "note", title: "Alpha" },
      { note_id: "b", file_kind: "note", title: "Beta" },
    ],
  } as { date: string; notes: unknown[] } | undefined,
};

describe("DayView", () => {
  beforeEach(() => {
    agendaQuery.current = { ...loadedAgenda };
    tasksQuery.current = { data: { tasks: [] } };
    navigate.mockClear();
    openJournal.mockClear();
  });

  it("lists the day's notes as plain links under the date heading", () => {
    const { container, getByText } = render(<DayView date="2026-07-03" />);

    expect(getByText("2026-07-03")).toBeTruthy();
    const links = [...container.querySelectorAll("a.backlink")];
    expect(links.map((a) => a.getAttribute("href"))).toEqual(["/notes/a", "/notes/b"]);
    expect(links.map((a) => a.textContent)).toEqual(["Alpha", "Beta"]);
  });

  it("shows an empty state when nothing was worked on", () => {
    agendaQuery.current = { ...loadedAgenda, data: { date: "2026-07-03", notes: [] } };
    const { getByText } = render(<DayView date="2026-07-03" />);
    expect(getByText("No notes were worked on this day.")).toBeTruthy();
  });

  it("offers adjacent days on an empty day without creating a journal", () => {
    agendaQuery.current = { ...loadedAgenda, data: { date: "2026-12-31", notes: [] } };
    const { getByRole } = render(<DayView date="2026-12-31" />);
    fireEvent.click(getByRole("button", { name: "Next day" }));
    expect(navigate).toHaveBeenLastCalledWith({ to: "/day/$date", params: { date: "2027-01-01" } });
    fireEvent.click(getByRole("button", { name: "Previous day" }));
    expect(navigate).toHaveBeenLastCalledWith({ to: "/day/$date", params: { date: "2026-12-30" } });
    expect(openJournal).not.toHaveBeenCalled();
  });

  it("opens (creating if needed) the day's journal from the header", async () => {
    openJournal.mockResolvedValue({ note_id: 20260703, created: true });
    const { getByText } = render(<DayView date="2026-07-03" />);

    fireEvent.click(getByText("Journal"));
    await waitFor(() => expect(navigate).toHaveBeenCalledWith({ to: "/notes/$noteId", params: { noteId: "20260703" } }));
    // "" is the working vault's default: the launch vault, with no switcher choice made.
    expect(openJournal).toHaveBeenCalledWith("2026-07-03", "");
  });

  it("lists the tasks planned for the day, marked by which date put them there", () => {
    tasksQuery.current = {
      data: {
        tasks: [
          { note_id: "9", file_kind: "note", title: "Plan", line: 1, state: "TODO", done: false, text: "ship it", due: "2026-07-03" },
          { note_id: "9", file_kind: "note", title: "Plan", line: 2, state: "TODO", done: false, text: "start it", scheduled: "2026-07-03" },
          { note_id: "9", file_kind: "note", title: "Plan", line: 3, state: "TODO", done: false, text: "elsewhere", due: "2026-07-04" },
        ],
      },
    };
    const { container, getByText } = render(<DayView date="2026-07-03" />);
    const rows = container.querySelectorAll(".day-task");
    expect(rows).toHaveLength(2);
    expect(getByText("ship it").parentElement?.textContent).toContain("!");
    expect(getByText("start it").parentElement?.textContent).toContain("▷");
  });

  it("rejects a malformed date", () => {
    const { getByText } = render(<DayView date="20260703" />);
    expect(getByText(/Invalid date/)).toBeTruthy();
  });

  it("filters day agenda sections while preserving activity and historical tasks", () => {
    tasksQuery.current = { data: { tasks: [{ note_id: "plan", title: "Plan", line: 1, state: "DONE", done: true, text: "Finished work", due: "2026-07-03" }] } };
    const { getByRole, getByText, queryByRole } = render(<DayView date="2026-07-03" />);
    const controls = within(getByRole("group", { name: "Agenda contents" }));
    fireEvent.click(controls.getByRole("button", { name: "Tasks" }));
    expect(getByText("Day appointment fixture")).not.toBeVisible();
    expect(getByRole("link", { name: /Finished work/ })).toBeVisible();
    expect(getByRole("link", { name: "Alpha" })).toBeVisible();
    expect(getByRole("button", { name: "Journal" })).toBeVisible();
    fireEvent.click(controls.getByRole("button", { name: "Events" }));
    expect(queryByRole("link", { name: /Finished work/ })).toBeNull();
    expect(getByText("Day appointment fixture")).toBeVisible();
    expect(openJournal).not.toHaveBeenCalled();
  });

  it("distinguishes loading, failed, and empty task data in task focus", () => {
    tasksQuery.current = { isPending: true };
    const { getByRole, getByText, rerender } = render(<DayView date="2026-07-03" />);
    fireEvent.click(within(getByRole("group", { name: "Agenda contents" })).getByRole("button", { name: "Tasks" }));
    expect(getByRole("status")).toHaveTextContent("Loading tasks...");
    tasksQuery.current = { isError: true, error: new Error("Task source unavailable") };
    rerender(<DayView date="2026-07-03" />);
    expect(getByRole("alert")).toHaveTextContent("Could not load tasks: Task source unavailable");
    tasksQuery.current = { data: { tasks: [] } };
    rerender(<DayView date="2026-07-03" />);
    expect(getByText("No dated tasks on this day.")).toBeVisible();
  });
});

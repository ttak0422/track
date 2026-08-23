import { fireEvent, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { CalendarFullView } from "./CalendarFullView";

// The calendar derives everything from the notes list's activity days. Journals carry no days (the
// engine excludes them); the month summary journal only feeds the title link.
const loadedNotes = {
  isPending: false,
  isError: false,
  error: null as Error | null,
  data: {
    notes: [
      { note_id: "a", file_kind: "note", title: "Alpha", days: ["2026-07-03", "2026-07-15"] },
      { note_id: "b", file_kind: "note", title: "Beta", days: ["2026-07-03"] },
      { note_id: "c", file_kind: "note", title: "Gamma", days: ["2026-07-03"] },
      { note_id: "d", file_kind: "note", title: "Delta", days: ["2026-07-03"] },
      { note_id: "m7", file_kind: "journal", title: "202607" },
    ],
  } as { notes: unknown[] } | undefined,
};

const notesQuery = vi.hoisted(() => ({ current: {} as Record<string, unknown> }));
const tasksQuery = vi.hoisted(() => ({ current: {} as Record<string, unknown> }));

vi.mock("../queries", () => ({
  useNotesQuery: () => notesQuery.current,
  useDatedTasksQuery: () => tasksQuery.current,
}));

// Link renders as a plain anchor carrying the resolved route path, so hrefs are assertable without a
// router context.
vi.mock("@tanstack/react-router", () => ({
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

describe("CalendarFullView", () => {
  beforeEach(() => {
    notesQuery.current = { ...loadedNotes };
    tasksQuery.current = { data: { tasks: [] } };
    vi.useFakeTimers();
    // A Sunday mid-month, so leading blanks and the today marker are both exercised.
    vi.setSystemTime(new Date(2026, 6, 5));
  });
  afterEach(() => vi.useRealTimers());

  it("renders the current month with active days linking to their day page", () => {
    const { container, getByText } = render(<CalendarFullView />);

    expect(getByText("2026 / 07")).toBeTruthy();
    // July 2026 starts on a Wednesday: three leading blanks, then 31 day cells.
    expect(container.querySelectorAll(".calendar-day-blank")).toHaveLength(3);
    expect(container.querySelectorAll(".calendar-day:not(.calendar-day-blank)")).toHaveLength(31);

    const links = [...container.querySelectorAll("a.calendar-day")];
    expect(links.map((a) => a.getAttribute("href"))).toEqual(["/day/2026-07-03", "/day/2026-07-15"]);
  });

  it("lists the top notes in a cell and collapses the rest into a count", () => {
    const { container } = render(<CalendarFullView />);

    const busy = container.querySelector('a[href="/day/2026-07-03"]')!;
    const titles = [...busy.querySelectorAll(".calendar-day-note")].map((n) => n.textContent);
    // Four notes on the 3rd: three titles shown, the fourth collapsed into "+1".
    expect(titles).toEqual(["Alpha", "Beta", "Gamma"]);
    expect(busy.querySelector(".calendar-day-more")?.textContent).toBe("+1");

    const light = container.querySelector('a[href="/day/2026-07-15"]')!;
    expect([...light.querySelectorAll(".calendar-day-note")].map((n) => n.textContent)).toEqual(["Alpha"]);
    expect(light.querySelector(".calendar-day-more")).toBeNull();
  });

  it("marks today and links the month title to the month journal", () => {
    const { container } = render(<CalendarFullView />);

    expect(container.querySelector('[aria-current="date"] .calendar-day-number')?.textContent).toBe("5");
    expect(container.querySelector("a.calendar-title")?.getAttribute("href")).toBe("/notes/m7");
  });

  it("navigates months and comes back with Today", () => {
    const { container, getByText, getByLabelText, queryByText } = render(<CalendarFullView />);

    fireEvent.click(getByLabelText("Previous month"));
    expect(getByText("2026 / 06")).toBeTruthy();
    // June 2026 has no activity and no month note: no day links, plain title.
    expect(container.querySelectorAll("a.calendar-day")).toHaveLength(0);
    expect(container.querySelector("a.calendar-title")).toBeNull();
    // Today's marker belongs to July, not June.
    expect(container.querySelector('[aria-current="date"]')).toBeNull();

    fireEvent.click(getByText("Today"));
    expect(getByText("2026 / 07")).toBeTruthy();
    expect(queryByText("2026 / 06")).toBeNull();
  });

  it("opens a day's journal when there is one, and the day page when there is not", () => {
    notesQuery.current = {
      ...loadedNotes,
      data: {
        notes: [
          ...(loadedNotes.data as { notes: unknown[] }).notes,
          { note_id: "j3", file_kind: "journal", title: "20260703", days: [] },
        ],
      },
    };
    const { container } = render(<CalendarFullView />);
    // A journal exists for the 3rd: the date opens it, since the day is what the journal is about.
    expect(container.querySelector('a.calendar-day[href="/notes/j3"]')).toBeTruthy();
    // No journal for the 15th, so the day page — clicking a date must never create a note.
    expect(container.querySelector('a.calendar-day[href="/day/2026-07-15"]')).toBeTruthy();
    expect(container.querySelector('a.calendar-day[href="/day/2026-07-03"]')).toBeNull();
  });

  it("lists dated tasks as rows before the note titles, collapsing the rest into a count", () => {
    tasksQuery.current = {
      data: {
        tasks: [
          // Both dates on one task: it belongs to two days, but only once to each.
          { note_id: "1", file_kind: "note", title: "Plan", line: 1, state: "TODO", done: false, text: "ship", scheduled: "2026-07-09", due: "2026-07-10" },
          { note_id: "2", file_kind: "note", title: "Plan", line: 2, state: "TODO", done: false, text: "write", due: "2026-07-10" },
          { note_id: "3", file_kind: "note", title: "Plan", line: 3, state: "TODO", done: false, text: "review", due: "2026-07-10" },
          { note_id: "4", file_kind: "note", title: "Plan", line: 4, state: "TODO", done: false, text: "polish", due: "2026-07-10" },
        ],
      },
    };
    const { container } = render(<CalendarFullView />);
    const cellFor = (day: string) =>
      container.querySelector(`a.calendar-day[href="/day/${day}"]`) as HTMLElement | null;
    // Tasks read as rows — the task's own text — not a bare "N tasks" count.
    expect(cellFor("2026-07-09")?.textContent).toContain("ship");
    const tenth = cellFor("2026-07-10")!;
    expect([...tenth.querySelectorAll(".calendar-day-task-text")].map((n) => n.textContent)).toEqual([
      "ship",
      "write",
      "review",
    ]);
    expect(tenth.querySelector(".calendar-day-task-more")?.textContent).toBe("+1");
    expect(tenth.textContent).not.toContain("task");
  });

  it("draws each task's deadline bar from the days left until its due date", () => {
    // System time is 2026-07-05, set in beforeEach; every bar is read against that day.
    const task = (line: number, dates: Partial<{ due: string; scheduled: string }>, text: string) => ({
      note_id: `t${line}`,
      file_kind: "note",
      title: "T",
      state: "TODO",
      done: false,
      ...dates,
      line,
      text,
    });
    tasksQuery.current = {
      data: {
        tasks: [
          task(1, { due: "2026-07-05" }, "today"),
          task(2, { due: "2026-07-12" }, "halfway"), // 7 of the 14 days left
          task(3, { due: "2026-07-19" }, "far out"), // at the window's edge
          task(4, { due: "2026-07-04" }, "late"), // past its deadline
          task(5, { scheduled: "2026-07-06" }, "unscheduled"), // no due date
        ],
      },
    };
    const { container } = render(<CalendarFullView />);
    const rowFor = (text: string) =>
      [...container.querySelectorAll(".calendar-day-task")].find((n) => n.textContent === text)!;

    // Due today: the window is spent, so the mark fill runs the whole track.
    expect(rowFor("today").querySelector(".calendar-day-due-fill")).toBeTruthy();
    expect(rowFor("today").querySelector(".calendar-day-due-overdue")).toBeNull();
    // Halfway through the window fills half the track; at the edge there is nothing left.
    expect(rowFor("halfway").querySelector<HTMLElement>(".calendar-day-due-fill")!.style.width).toBe("50%");
    expect(rowFor("far out").querySelector<HTMLElement>(".calendar-day-due-fill")!.style.width).toBe("0%");
    // Overdue takes the full track under the danger treatment.
    expect(
      rowFor("late").querySelector<HTMLElement>(".calendar-day-due-overdue .calendar-day-due-fill")!.style
        .width,
    ).toBe("100%");
    // Scheduled only: a period with no end draws no bar.
    expect(rowFor("unscheduled").querySelector(".calendar-day-due")).toBeNull();
  });

  it("shows the pending state before the notes list resolves", () => {
    notesQuery.current = { ...loadedNotes, isPending: true, data: undefined };
    const { container, getByText } = render(<CalendarFullView />);
    expect(getByText("Loading calendar...")).toBeTruthy();
    expect(container.querySelector(".calendar-grid")).toBeNull();
  });
});

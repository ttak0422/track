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

  it("counts dated tasks on their day, and makes a purely planned day openable", () => {
    tasksQuery.current = {
      data: {
        tasks: [
          // Both dates on one task: it belongs to two days, but only once to each.
          { note_id: "1", file_kind: "note", title: "Plan", line: 1, state: "TODO", done: false, text: "ship", scheduled: "2026-07-09", due: "2026-07-10" },
          { note_id: "2", file_kind: "note", title: "Plan", line: 2, state: "TODO", done: false, text: "write", due: "2026-07-10" },
        ],
      },
    };
    const { container } = render(<CalendarFullView />);
    const cellFor = (day: string) =>
      container.querySelector(`a.calendar-day[href="/day/${day}"]`) as HTMLElement | null;
    // 2026-07-09 has no note activity in the fixture, so only the task puts it on the calendar.
    expect(cellFor("2026-07-09")?.textContent).toContain("1 task");
    expect(cellFor("2026-07-10")?.textContent).toContain("2 tasks");
  });

  it("shows the pending state before the notes list resolves", () => {
    notesQuery.current = { ...loadedNotes, isPending: true, data: undefined };
    const { container, getByText } = render(<CalendarFullView />);
    expect(getByText("Loading calendar...")).toBeTruthy();
    expect(container.querySelector(".calendar-grid")).toBeNull();
  });
});

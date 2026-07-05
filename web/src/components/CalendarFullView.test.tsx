import { fireEvent, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { CalendarFullView } from "./CalendarFullView";

// The calendar derives everything from the notes list; feed it a fixed set with day journals, a month
// summary, and a decoy note whose title looks like a date but is not a journal.
const loadedNotes = {
  isPending: false,
  isError: false,
  error: null as Error | null,
  data: {
    notes: [
      { note_id: "j3", file_kind: "journal", title: "20260703" },
      { note_id: "j15", file_kind: "journal", title: "20260715" },
      { note_id: "m7", file_kind: "journal", title: "202607" },
      { note_id: "n1", file_kind: "note", title: "20260710" },
    ],
  } as { notes: unknown[] } | undefined,
};

const notesQuery = vi.hoisted(() => ({ current: {} as Record<string, unknown> }));

vi.mock("../queries", () => ({ useNotesQuery: () => notesQuery.current }));

// Link renders as a plain anchor carrying the resolved note path, so hrefs are assertable without a
// router context.
vi.mock("@tanstack/react-router", () => ({
  Link: ({
    to,
    params,
    children,
    ...rest
  }: {
    to: string;
    params?: { noteId?: string };
    children: React.ReactNode;
  } & Record<string, unknown>) => (
    <a href={to.replace("$noteId", params?.noteId ?? "")} {...rest}>
      {children}
    </a>
  ),
}));

describe("CalendarFullView", () => {
  beforeEach(() => {
    notesQuery.current = { ...loadedNotes };
    vi.useFakeTimers();
    // A Sunday mid-month, so leading blanks and the today marker are both exercised.
    vi.setSystemTime(new Date(2026, 6, 5));
  });
  afterEach(() => vi.useRealTimers());

  it("renders the current month with journal days linked and other days inert", () => {
    const { container, getByText } = render(<CalendarFullView />);

    expect(getByText("July 2026")).toBeTruthy();
    // July 2026 starts on a Wednesday: three leading blanks, then 31 day cells.
    expect(container.querySelectorAll(".calendar-day-blank")).toHaveLength(3);
    expect(container.querySelectorAll(".calendar-day:not(.calendar-day-blank)")).toHaveLength(31);

    const links = [...container.querySelectorAll("a.calendar-day")];
    expect(links.map((a) => a.getAttribute("href"))).toEqual(["/notes/j3", "/notes/j15"]);
    // The look-alike plain note (20260710) must not make day 10 a link.
    expect(links.map((a) => a.textContent)).toEqual(["3", "15"]);
  });

  it("marks today and links the month title to the month journal", () => {
    const { container } = render(<CalendarFullView />);

    expect(container.querySelector('[aria-current="date"]')?.textContent).toBe("5");
    expect(container.querySelector("a.calendar-title")?.getAttribute("href")).toBe("/notes/m7");
  });

  it("navigates months and comes back with Today", () => {
    const { container, getByText, getByLabelText, queryByText } = render(<CalendarFullView />);

    fireEvent.click(getByLabelText("Previous month"));
    expect(getByText("June 2026")).toBeTruthy();
    // June 2026 has no journals and no month note: no day links, plain title.
    expect(container.querySelectorAll("a.calendar-day")).toHaveLength(0);
    expect(container.querySelector("a.calendar-title")).toBeNull();
    // Today's marker belongs to July, not June.
    expect(container.querySelector('[aria-current="date"]')).toBeNull();

    fireEvent.click(getByText("Today"));
    expect(getByText("July 2026")).toBeTruthy();
    expect(queryByText("June 2026")).toBeNull();
  });

  it("shows the pending state before the notes list resolves", () => {
    notesQuery.current = { ...loadedNotes, isPending: true, data: undefined };
    const { container, getByText } = render(<CalendarFullView />);
    expect(getByText("Loading calendar...")).toBeTruthy();
    expect(container.querySelector(".calendar-grid")).toBeNull();
  });
});

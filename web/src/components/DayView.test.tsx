import { fireEvent, render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { DayView } from "./DayView";

const agendaQuery = vi.hoisted(() => ({ current: {} as Record<string, unknown> }));
const navigate = vi.hoisted(() => vi.fn());
const openJournal = vi.hoisted(() => vi.fn());

vi.mock("../queries", () => ({
  useAgendaQuery: () => agendaQuery.current,
  useResolveQuery: () => ({ data: undefined }),
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

  it("opens (creating if needed) the day's journal from the header", async () => {
    openJournal.mockResolvedValue({ note_id: 20260703, created: true });
    const { getByText } = render(<DayView date="2026-07-03" />);

    fireEvent.click(getByText("Journal"));
    await waitFor(() => expect(navigate).toHaveBeenCalledWith({ to: "/notes/$noteId", params: { noteId: "20260703" } }));
    expect(openJournal).toHaveBeenCalledWith("2026-07-03");
  });

  it("rejects a malformed date", () => {
    const { getByText } = render(<DayView date="20260703" />);
    expect(getByText(/Invalid date/)).toBeTruthy();
  });
});

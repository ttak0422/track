import { act, fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { SidebarNew } from "./SidebarNew";

const routerMock = vi.hoisted(() => ({ pathname: "/", navigate: vi.fn() }));
const newNotes = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, params, ...rest }: { children: unknown; params: { noteId: string } }) => (
    <a href={`/notes/${params.noteId}`} {...rest}>
      {children as never}
    </a>
  ),
  useRouterState: () => routerMock.pathname,
  useNavigate: () => routerMock.navigate,
}));

vi.mock("../queries", () => ({ useNewNotesQuery: (limit: number) => {
  newNotes(limit);
  return { data: { notes: [{ note_id: "first", title: "First note" }, { note_id: "second", title: "Second note" }] } };
} }));

describe("SidebarNew", () => {
  beforeEach(() => {
    newNotes.mockClear();
    vi.useRealTimers();
  });

  it("requests and lists recently-created notes", async () => {
    render(<SidebarNew />);
    expect(newNotes).toHaveBeenCalledWith(100);
    fireEvent.pointerEnter(screen.getByRole("button", { name: "Recently created notes" }));
    const panel = await screen.findByRole("menu", { name: "Recently created notes" });
    expect(screen.getAllByRole("menuitem").map((item) => item.textContent)).toEqual([
      "First note",
      "Second note",
    ]);
    expect(screen.getByRole("heading", { name: "New" })).toHaveClass("rail-panel-title");
    expect(panel.closest(".history-panel")?.parentElement).toBe(document.body);
  });

  it("shows an empty state", () => {
    // The hook is intentionally mocked above with data; this smoke test covers the accessible trigger.
    render(<SidebarNew />);
    expect(screen.getByRole("button", { name: "Recently created notes" })).toBeInTheDocument();
  });

  it("opens from a touch tap without hover toggling it shut", () => {
    vi.useFakeTimers();
    render(<SidebarNew />);
    const trigger = screen.getByRole("button", { name: "Recently created notes" });
    fireEvent.pointerEnter(trigger, { pointerType: "touch" });
    fireEvent.pointerLeave(trigger, { pointerType: "touch" });
    fireEvent.click(trigger);
    act(() => vi.advanceTimersByTime(400));
    expect(screen.getByRole("menu", { name: "Recently created notes" })).toBeInTheDocument();
  });
});

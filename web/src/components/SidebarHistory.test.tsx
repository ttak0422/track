import { act, fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { SidebarHistory } from "./SidebarHistory";
import { TabsProvider } from "./tabs/tabsStore";

const routerMock = vi.hoisted(() => ({ pathname: "/", navigate: vi.fn() }));

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, params, ...rest }: { children: unknown; params: { noteId: string } }) => (
    <a href={`/notes/${params.noteId}`} {...rest}>
      {children as never}
    </a>
  ),
  useRouterState: () => routerMock.pathname,
  useNavigate: () => routerMock.navigate,
}));

function renderHistory() {
  return render(
    <TabsProvider>
      <aside className="sidebar">
        <SidebarHistory />
      </aside>
    </TabsProvider>,
  );
}

describe("SidebarHistory", () => {
  beforeEach(() => {
    routerMock.pathname = "/";
    routerMock.navigate.mockClear();
    window.localStorage.clear();
    vi.useRealTimers();
  });

  it("lists the store's recently opened notes in most-recent-first order", async () => {
    window.localStorage.setItem(
      "track.recent",
      JSON.stringify([
        { id: "first", title: "First note" },
        { id: "second", title: "Second note" },
      ]),
    );
    renderHistory();

    fireEvent.pointerEnter(screen.getByRole("button", { name: "Recently opened notes" }));
    const panel = await screen.findByRole("menu", { name: "Recently opened notes" });
    expect(screen.getAllByRole("menuitem").map((item) => item.textContent)).toEqual([
      "First note",
      "Second note",
    ]);
    expect(screen.getByRole("menuitem", { name: "First note" })).toHaveAttribute(
      "href",
      "/notes/first",
    );
    expect(panel.parentElement).toBe(document.body);
    expect(panel.closest(".sidebar")).toBeNull();
  });

  it("explains the empty history when there are no recent notes", () => {
    renderHistory();

    fireEvent.pointerEnter(screen.getByRole("button", { name: "Recently opened notes" }));
    expect(screen.getByText("No recently opened notes")).toBeInTheDocument();
    expect(screen.queryByRole("menuitem")).not.toBeInTheDocument();
  });

  it("keeps the panel open across the trigger-to-panel gap, then closes after leaving", () => {
    vi.useFakeTimers();
    renderHistory();
    const trigger = screen.getByRole("button", { name: "Recently opened notes" });
    fireEvent.pointerEnter(trigger);
    const panel = screen.getByRole("menu", { name: "Recently opened notes" });

    fireEvent.pointerLeave(trigger);
    fireEvent.pointerEnter(panel);
    act(() => vi.advanceTimersByTime(200));
    expect(panel).toBeInTheDocument();

    fireEvent.pointerLeave(panel);
    act(() => vi.advanceTimersByTime(200));
    expect(screen.queryByRole("menu", { name: "Recently opened notes" })).not.toBeInTheDocument();
  });

  it("opens from keyboard focus and closes on Escape", () => {
    renderHistory();
    const trigger = screen.getByRole("button", { name: "Recently opened notes" });

    fireEvent.focus(trigger);
    expect(screen.getByRole("menu", { name: "Recently opened notes" })).toBeInTheDocument();

    fireEvent.keyDown(document, { key: "Escape" });
    expect(screen.queryByRole("menu", { name: "Recently opened notes" })).not.toBeInTheDocument();
    expect(trigger).toHaveAttribute("aria-expanded", "false");
  });

  it("toggles from a direct click", () => {
    renderHistory();
    const trigger = screen.getByRole("button", { name: "Recently opened notes" });

    fireEvent.click(trigger);
    expect(screen.getByRole("menu", { name: "Recently opened notes" })).toBeInTheDocument();
    fireEvent.click(trigger);
    expect(screen.queryByRole("menu", { name: "Recently opened notes" })).not.toBeInTheDocument();
  });
});

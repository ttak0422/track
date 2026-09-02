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
    // The rail is a fixed stacking context, so the panel has to be a body child to outrank previews.
    const surface = panel.closest(".history-panel")!;
    expect(surface.parentElement).toBe(document.body);
    expect(surface.closest(".sidebar")).toBeNull();
    // The heading names the panel, and stays out of the menu where only menu items belong.
    const title = screen.getByRole("heading", { name: "History" });
    expect(title).toHaveClass("rail-panel-title");
    expect(title.closest('[role="menu"]')).toBeNull();
    // That heading is the panel's whole name: the glyph does not repeat it in a native tooltip.
    expect(screen.getByRole("button", { name: "Recently opened notes" })).not.toHaveAttribute(
      "title",
    );
    // A row inside the panel keeps its title — the rows clip, and it is what reveals the rest.
    expect(screen.getByRole("menuitem", { name: "First note" })).toHaveAttribute(
      "title",
      "First note",
    );
  });

  it("explains the empty history when there are no recent notes", () => {
    renderHistory();

    fireEvent.pointerEnter(screen.getByRole("button", { name: "Recently opened notes" }));
    expect(screen.getByText("No recently opened notes")).toBeInTheDocument();
    expect(screen.queryByRole("menuitem")).not.toBeInTheDocument();
  });

  // History is title-based, never id-based: a recents entry whose title has not resolved (a deleted
  // note, or one hopped away from before the query returned) reads as "Untitled" like a tab in the
  // strip, not as a bare internal id — a timestamp or slug is not a label.
  it("labels an untitled entry Untitled rather than its id", async () => {
    window.localStorage.setItem(
      "track.recent",
      JSON.stringify([
        { id: "titled", title: "Titled note" },
        { id: "1781359469000", title: "" },
      ]),
    );
    renderHistory();

    fireEvent.pointerEnter(screen.getByRole("button", { name: "Recently opened notes" }));
    const panel = await screen.findByRole("menu", { name: "Recently opened notes" });
    expect(panel).toHaveTextContent("Titled note");
    expect(panel).toHaveTextContent("Untitled");
    expect(panel).not.toHaveTextContent("1781359469000");
    // The entry still routes by id; only its label hides the id.
    expect(screen.getByRole("menuitem", { name: "Untitled" })).toHaveAttribute(
      "href",
      "/notes/1781359469000",
    );
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

    // A real focus, not a synthesized event: what opens the panel is keyboard focus specifically
    // (:focus-visible), and only actually focusing the button puts it in that state.
    act(() => trigger.focus());
    expect(screen.getByRole("menu", { name: "Recently opened notes" })).toBeInTheDocument();

    fireEvent.keyDown(document, { key: "Escape" });
    expect(screen.queryByRole("menu", { name: "Recently opened notes" })).not.toBeInTheDocument();
    expect(trigger).toHaveAttribute("aria-expanded", "false");
  });

  // A tap fires pointerenter on the way down and pointerleave on the way up, so the panel opened
  // under the finger and the button's own click found it open and closed it again — on a phone the
  // history could not be opened at all. A touch pointer is left to the click alone.
  it("opens from a tap, which is not a hover", () => {
    vi.useFakeTimers();
    renderHistory();
    const trigger = screen.getByRole("button", { name: "Recently opened notes" });

    fireEvent.pointerEnter(trigger, { pointerType: "touch" });
    fireEvent.pointerLeave(trigger, { pointerType: "touch" });
    fireEvent.click(trigger);
    act(() => vi.advanceTimersByTime(400));

    expect(screen.getByRole("menu", { name: "Recently opened notes" })).toBeInTheDocument();
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

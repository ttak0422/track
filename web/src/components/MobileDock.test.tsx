import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { MobileDock } from "./MobileDock";

const navigate = vi.hoisted(() => vi.fn());
vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => navigate,
  Link: ({ children, ...rest }: { children?: React.ReactNode }) => <a {...rest}>{children}</a>,
}));
vi.mock("../queries", () => ({
  useSiteQuery: () => ({ data: { icon: undefined, calendar: false } }),
}));
vi.mock("../runtime", () => ({ STATIC_MODE: false }));
vi.mock("../api", () => ({ openJournal: vi.fn() }));
vi.mock("./tabs/tabsStore", () => ({
  useTabs: () => ({ recent: [{ id: "100", title: "Alpha" }] }),
}));
vi.mock("./SearchPanel", () => ({
  SearchPanel: () => <div aria-label="Search notes">Search panel</div>,
}));

// jsdom gives the window 1024x768; the mark starts in the bottom-right corner, so the fan (which
// opens away from the nearest edge) fans upward from there.
function dock() {
  return render(<MobileDock />);
}

describe("MobileDock", () => {
  it("starts closed with only the mark", () => {
    const { container } = dock();
    expect(container.querySelector(".mobile-dock-fab")).not.toBeNull();
    expect(container.querySelector(".mobile-dock-fan-btn")).toBeNull();
  });

  it("a tap on the mark opens the fan, with the expected actions", () => {
    const { container } = dock();
    fireEvent.pointerDown(container.querySelector(".mobile-dock-fab")!);
    fireEvent.pointerUp(container.querySelector(".mobile-dock-fab")!);

    const labels = [...container.querySelectorAll(".mobile-dock-fan-btn")].map((b) =>
      b.getAttribute("aria-label"),
    );
    expect(labels).toContain("Search notes");
    expect(labels).toContain("Recently opened notes");
    expect(labels).toContain("Today's journal");
    expect(labels).toContain("Calendar");
    expect(labels).toContain("Tasks");
    expect(labels).toContain("Full graph");
    expect(labels).toContain("Settings");
  });

  it("a second tap closes the fan", () => {
    const { container } = dock();
    const fab = container.querySelector(".mobile-dock-fab")!;
    fireEvent.pointerDown(fab);
    fireEvent.pointerUp(fab);
    expect(container.querySelector(".mobile-dock-fan-btn")).not.toBeNull();
    fireEvent.pointerDown(fab);
    fireEvent.pointerUp(fab);
    expect(container.querySelector(".mobile-dock-fan-btn")).toBeNull();
  });

  // An untouched mark carries no inline offsets: the stylesheet's own right/bottom park it, so it
  // follows a window that changes size without a re-render.
  it("leaves the untouched mark's corner to the stylesheet", () => {
    const { container } = dock();
    expect(container.querySelector(".mobile-dock-fab")!.getAttribute("style")).toBeNull();
  });

  it("a drag moves the mark without opening the fan", () => {
    const { container } = dock();
    const fab = container.querySelector(".mobile-dock-fab") as HTMLElement;
    // The mark starts at the bottom-right corner of jsdom's 1024x768 window. (jsdom's PointerEvent
    // drops clientX, so the move is asserted on the axis it carries.)
    fireEvent.pointerDown(fab, { clientY: 700 });
    fireEvent.pointerMove(fab, { clientY: 640 });
    expect(fab.getAttribute("style")).toContain("top: 648px");
    // Each move is relative to the one before it, not to where the thumb first landed — the mark
    // travels exactly as far as the thumb does rather than adding the whole travel again.
    fireEvent.pointerMove(fab, { clientY: 620 });
    fireEvent.pointerUp(fab, { clientY: 620 });

    expect(container.querySelector(".mobile-dock-fan-btn")).toBeNull();
    expect(fab.getAttribute("style")).toContain("top: 628px");
  });

  it("brings a dragged mark back inside a window that shrinks under it", () => {
    const { container } = dock();
    const fab = container.querySelector(".mobile-dock-fab") as HTMLElement;
    fireEvent.pointerDown(fab, { clientY: 700 });
    fireEvent.pointerMove(fab, { clientY: 690 });
    fireEvent.pointerUp(fab, { clientY: 690 });
    expect(fab.getAttribute("style")).toContain("top: 698px");

    Object.defineProperty(window, "innerHeight", { value: 400, configurable: true });
    fireEvent(window, new Event("resize"));
    // 400 - 48 (the mark) - 12 (its edge): the foot of the new window, not off the end of it.
    expect(fab.getAttribute("style")).toContain("top: 340px");
    Object.defineProperty(window, "innerHeight", { value: 768, configurable: true });
  });

  it("opens the search palette from the fan", () => {
    const { container } = dock();
    const fab = container.querySelector(".mobile-dock-fab")!;
    fireEvent.pointerDown(fab);
    fireEvent.pointerUp(fab);

    fireEvent.click(screen.getByRole("button", { name: "Search notes" }));
    expect(screen.getByRole("dialog", { name: "Search notes" })).not.toBeNull();
    // The fan closes when a popup opens.
    expect(container.querySelector(".mobile-dock-fan-btn")).toBeNull();
  });

  it("lists recent notes from the history popup", () => {
    const { container } = dock();
    const fab = container.querySelector(".mobile-dock-fab")!;
    fireEvent.pointerDown(fab);
    fireEvent.pointerUp(fab);

    fireEvent.click(screen.getByRole("button", { name: "Recently opened notes" }));
    expect(screen.getByText("Alpha")).not.toBeNull();
  });

  it("applies a theme picked in the settings popup", () => {
    const { container } = dock();
    const fab = container.querySelector(".mobile-dock-fab")!;
    fireEvent.pointerDown(fab);
    fireEvent.pointerUp(fab);

    fireEvent.click(screen.getByRole("button", { name: "Settings" }));
    fireEvent.click(screen.getByRole("button", { name: "Dark", pressed: false }));
    expect(document.documentElement.dataset.theme).toBe("dark");
    expect(localStorage.getItem("track.theme")).toBe("dark");
  });

  it("closes on an outside tap", async () => {
    const { container } = dock();
    const fab = container.querySelector(".mobile-dock-fab")!;
    fireEvent.pointerDown(fab);
    fireEvent.pointerUp(fab);
    expect(container.querySelector(".mobile-dock-fan-btn")).not.toBeNull();

    fireEvent.mouseDown(document.body);
    await waitFor(() => expect(container.querySelector(".mobile-dock-fan-btn")).toBeNull());
  });

  it("navigates to the calendar from the fan", () => {
    const { container } = dock();
    const fab = container.querySelector(".mobile-dock-fab")!;
    fireEvent.pointerDown(fab);
    fireEvent.pointerUp(fab);

    fireEvent.click(screen.getByRole("button", { name: "Calendar" }));
    expect(navigate).toHaveBeenCalledWith({ to: "/calendar" });
  });
});

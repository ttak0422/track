import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useEffect } from "react";
import { describe, expect, it, vi } from "vitest";
import { NoteControlsProvider, useNoteControls, type NoteActions } from "../noteControls";
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
function dock(noteActions?: NoteActions) {
  return render(
    <NoteControlsProvider>
      {noteActions ? <OpenNote actions={noteActions} /> : null}
      <MobileDock />
    </NoteControlsProvider>,
  );
}

// Stands in for the note view, which hands the workspace its actions while it is mounted.
function OpenNote({ actions }: { actions: NoteActions }) {
  const { setActions } = useNoteControls();
  useEffect(() => setActions(actions), [actions, setActions]);
  return null;
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
    // The note group belongs to an open note, exactly as it does in the rail.
    expect(labels).not.toContain("This note");
  });

  // On a phone the rail is behind the mark, so the fan is the only way to the open note's own
  // controls — follow, the display mode, and the two dialogs.
  it("reaches the open note's controls through the fan", () => {
    const onMeta = vi.fn();
    const onDelete = vi.fn();
    const { container } = dock({ getBody: () => "", onMeta, onDelete });
    const fab = container.querySelector(".mobile-dock-fab")!;
    fireEvent.pointerDown(fab);
    fireEvent.pointerUp(fab);
    fireEvent.click(screen.getByRole("button", { name: "This note" }));

    fireEvent.click(screen.getByRole("button", { name: /^Follow the editor: Off/ }));
    expect(screen.getByRole("button", { name: /^Follow the editor: On/ })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Edit", pressed: false }));
    expect(screen.getByRole("button", { name: "Edit", pressed: true })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Meta…" }));
    expect(onMeta).toHaveBeenCalled();
    // The dialog it opens owns the screen from here, so the panel closes behind it.
    expect(screen.queryByRole("button", { name: "Meta…" })).not.toBeInTheDocument();
  });

  it("hands delete straight to the note's own dialog", () => {
    const onDelete = vi.fn();
    const { container } = dock({ getBody: () => "", onMeta: vi.fn(), onDelete });
    const fab = container.querySelector(".mobile-dock-fab")!;
    fireEvent.pointerDown(fab);
    fireEvent.pointerUp(fab);
    fireEvent.click(screen.getByRole("button", { name: "This note" }));
    fireEvent.click(screen.getByRole("button", { name: "Delete…" }));
    expect(onDelete).toHaveBeenCalled();
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

  // The mark starts in a corner, where the arc has a quadrant to fan into rather than a half circle.
  // The buttons have to stand apart on it: an arc too short for them ended with the off-screen clamp
  // stacking half the fan on a single point, where only the topmost one could be tapped at all.
  it("stands the fan's buttons apart even in a corner", () => {
    const { container } = dock({ getBody: () => "", onMeta: vi.fn(), onDelete: vi.fn() });
    const fab = container.querySelector(".mobile-dock-fab")!;
    fireEvent.pointerDown(fab);
    fireEvent.pointerUp(fab);

    const spots = [...container.querySelectorAll<HTMLElement>(".mobile-dock-fan-btn")].map((b) => ({
      x: Number.parseFloat(b.style.left),
      y: Number.parseFloat(b.style.top),
    }));
    expect(spots).toHaveLength(8);
    for (const [i, a] of spots.entries()) {
      // Inside jsdom's 1024x768 window, every one of them.
      expect(a.x).toBeGreaterThanOrEqual(8);
      expect(a.y).toBeGreaterThanOrEqual(8);
      expect(a.x).toBeLessThanOrEqual(1024 - 44 - 8);
      expect(a.y).toBeLessThanOrEqual(768 - 44 - 8);
      for (const b of spots.slice(i + 1)) {
        expect(Math.hypot(a.x - b.x, a.y - b.y)).toBeGreaterThanOrEqual(44);
      }
    }
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

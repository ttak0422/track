import { act, fireEvent, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { GraphFullView, graphPointAnchor } from "./GraphFullView";
import { previewOpenDelay } from "./preview/stack";

const floatingOpen = vi.hoisted(() => vi.fn());
const navigate = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-router", () => ({ useNavigate: () => navigate }));
vi.mock("../queries", () => ({ useGraphQuery: () => ({ data: { graph: { nodes: [], edges: [] } } }) }));
vi.mock("./preview/floatingStore", () => ({ useFloating: () => ({ open: floatingOpen }) }));

// Stub the canvas so the test can drive onHover/onSelect directly, and the note window so it exposes the
// detach/pin/close controls the hover machine wires up.
vi.mock("./GraphCanvas", () => ({
  GraphCanvas: ({
    onHover,
    onSelect,
  }: {
    onHover?: (id: string | null, p: { x: number; y: number }) => void;
    onSelect: (id: string) => void;
  }) => (
    <div>
      <button type="button" onClick={() => onHover?.("a", { x: 10, y: 10 })}>
        hover-a
      </button>
      <button type="button" onClick={() => onHover?.("b", { x: 20, y: 20 })}>
        hover-b
      </button>
      <button type="button" onClick={() => onHover?.(null, { x: 0, y: 0 })}>
        hover-out
      </button>
      <button type="button" onClick={() => onSelect("a")}>
        select-a
      </button>
    </div>
  ),
}));

vi.mock("./preview/NoteWindow", () => ({
  NoteWindow: ({
    noteID,
    onDetach,
    onLeave,
    onClose,
    onPinToggle,
  }: {
    noteID: string;
    onDetach?: () => void;
    onLeave?: () => void;
    onClose: () => void;
    onPinToggle: (b: { left: number; top: number; width: number; height: number }, c: boolean) => void;
  }) => (
    <div data-testid="note-window" data-note-id={noteID}>
      <button type="button" onClick={() => onDetach?.()}>
        detach
      </button>
      <button type="button" onClick={() => onLeave?.()}>
        leave
      </button>
      <button type="button" onClick={() => onPinToggle({ left: 0, top: 0, width: 300, height: 200 }, false)}>
        pin
      </button>
      <button type="button" onClick={onClose}>
        close
      </button>
    </div>
  ),
}));

describe("graphPointAnchor", () => {
  it("uses the clicked graph point as the floating preview anchor", () => {
    expect(graphPointAnchor({ x: 320, y: 180 })).toEqual({
      linkLeft: 320,
      linkRight: 320,
      linkTop: 180,
      linkBottom: 180,
    });
  });
});

describe("GraphFullView hover preview", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    floatingOpen.mockClear();
    navigate.mockClear();
  });
  afterEach(() => vi.useRealTimers());

  function win(c: HTMLElement) {
    return c.querySelector('[data-testid="note-window"]');
  }
  function click(c: HTMLElement, label: string) {
    fireEvent.click([...c.querySelectorAll("button")].find((b) => b.textContent?.trim() === label)!);
  }

  it("opens a preview after the intent delay and closes it when the pointer leaves", () => {
    const { container } = render(<GraphFullView />);
    click(container, "hover-a");
    expect(win(container)).toBeNull(); // still within the intent delay
    act(() => vi.advanceTimersByTime(previewOpenDelay + 10));
    expect(win(container)?.getAttribute("data-note-id")).toBe("a");

    click(container, "hover-out");
    act(() => vi.advanceTimersByTime(300));
    expect(win(container)).toBeNull();
  });

  it("cancels a pending open when the pointer leaves before the delay", () => {
    const { container } = render(<GraphFullView />);
    click(container, "hover-a");
    act(() => vi.advanceTimersByTime(previewOpenDelay - 50));
    click(container, "hover-out");
    act(() => vi.advanceTimersByTime(previewOpenDelay + 300));
    expect(win(container)).toBeNull();
  });

  it("closes a hover preview when the pointer leaves it without dragging", () => {
    const { container } = render(<GraphFullView />);
    click(container, "hover-a");
    act(() => vi.advanceTimersByTime(previewOpenDelay + 10));
    expect(win(container)).not.toBeNull();

    click(container, "leave"); // pointer leaves the window, no drag
    act(() => vi.advanceTimersByTime(300));
    expect(win(container)).toBeNull();
  });

  it("keeps a dragged (sticky) preview open after the pointer leaves", () => {
    const { container } = render(<GraphFullView />);
    click(container, "hover-a");
    act(() => vi.advanceTimersByTime(previewOpenDelay + 10));
    click(container, "detach");
    click(container, "hover-out");
    act(() => vi.advanceTimersByTime(300));
    expect(win(container)).not.toBeNull();
    click(container, "close");
    expect(win(container)).toBeNull();
  });

  it("hands a dragged preview to the floating layer (unpinned) when another node is hovered", () => {
    const { container } = render(<GraphFullView />);
    click(container, "hover-a");
    act(() => vi.advanceTimersByTime(previewOpenDelay + 10));
    click(container, "detach"); // keep window a without pinning
    click(container, "hover-b");
    // a is handed off unpinned so the slot frees up...
    expect(floatingOpen).toHaveBeenCalledWith(
      { kind: "note", noteID: "a" },
      expect.anything(),
      false,
      false,
    );
    // ...and b pops in the freed transient slot.
    act(() => vi.advanceTimersByTime(previewOpenDelay + 10));
    expect(win(container)?.getAttribute("data-note-id")).toBe("b");
  });

  it("promotes to the floating layer on pin", () => {
    const { container } = render(<GraphFullView />);
    click(container, "hover-a");
    act(() => vi.advanceTimersByTime(previewOpenDelay + 10));
    click(container, "pin");
    expect(floatingOpen).toHaveBeenCalledWith(
      { kind: "note", noteID: "a" },
      { left: 0, top: 0, width: 300, height: 200 },
      false,
      true,
    );
    expect(win(container)).toBeNull();
  });

  it("navigates to the note on click", () => {
    const { container } = render(<GraphFullView />);
    click(container, "select-a");
    expect(navigate).toHaveBeenCalledWith({ to: "/notes/$noteId", params: { noteId: "a" } });
  });
});

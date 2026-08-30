import { act, fireEvent, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { GraphFullView, graphPointAnchor } from "./GraphFullView";
import { previewOpenDelay } from "./preview/stack";

// The view no longer owns a window: it asks the floating layer to open one and keeps the id. So the
// stand-in is the layer's api, and what the tests read is what the view asked it for.
const floating = vi.hoisted(() => ({
  windows: [] as { id: string; content: { kind: string; noteID: string } }[],
  open: vi.fn(() => "win-1"),
  hold: vi.fn(),
  scheduleClose: vi.fn(),
}));
const navigate = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-router", () => ({ useNavigate: () => navigate }));
vi.mock("../queries", () => ({ useGraphQuery: () => ({ data: { graph: { nodes: [], edges: [] } } }) }));
vi.mock("./preview/floatingStore", () => ({ useFloating: () => floating }));

// Stub the canvas so the test can drive onHover/onSelect directly. GraphFullView consumes the canvas
// through the lazy wrapper (GraphCanvasLazy), so mock that module — it renders synchronously here,
// bypassing Suspense.
vi.mock("./GraphCanvasLazy", () => ({
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
    floating.windows = [];
    floating.open.mockClear();
    floating.hold.mockClear();
    floating.scheduleClose.mockClear();
    navigate.mockClear();
  });
  afterEach(() => vi.useRealTimers());

  function click(c: HTMLElement, label: string) {
    fireEvent.click([...c.querySelectorAll("button")].find((b) => b.textContent?.trim() === label)!);
  }

  it("opens a transient window in the layer after the intent delay", () => {
    const { container } = render(<GraphFullView />);
    click(container, "hover-a");
    expect(floating.open).not.toHaveBeenCalled(); // still within the intent delay

    act(() => vi.advanceTimersByTime(previewOpenDelay + 10));
    expect(floating.open).toHaveBeenCalledWith(
      { kind: "note", noteID: "a" },
      expect.anything(),
      false,
      { transient: true, anchor: { linkLeft: 10, linkRight: 10, linkTop: 10, linkBottom: 10 } },
    );
  });

  it("cancels a pending open when the pointer leaves before the delay", () => {
    const { container } = render(<GraphFullView />);
    click(container, "hover-a");
    act(() => vi.advanceTimersByTime(previewOpenDelay - 50));
    click(container, "hover-out");
    act(() => vi.advanceTimersByTime(previewOpenDelay + 300));
    expect(floating.open).not.toHaveBeenCalled();
  });

  it("asks the layer to close the window it opened when the pointer leaves", () => {
    const { container } = render(<GraphFullView />);
    click(container, "hover-a");
    act(() => vi.advanceTimersByTime(previewOpenDelay + 10));
    click(container, "hover-out");
    expect(floating.scheduleClose).toHaveBeenCalledWith("win-1");
  });

  // Resting on the node already shown holds its window where it is instead of re-anchoring it to
  // every pixel the cursor moves.
  it("holds the open window rather than chasing the cursor on the same node", () => {
    const { container } = render(<GraphFullView />);
    click(container, "hover-a");
    act(() => vi.advanceTimersByTime(previewOpenDelay + 10));
    expect(floating.open).toHaveBeenCalledTimes(1);

    floating.windows = [{ id: "win-1", content: { kind: "note", noteID: "a" } }];
    click(container, "hover-a");
    act(() => vi.advanceTimersByTime(previewOpenDelay + 10));
    expect(floating.open).toHaveBeenCalledTimes(1);
    expect(floating.hold).toHaveBeenCalledWith("win-1");
  });

  // A different node is a different window: the layer holds both, ordered by whichever was opened
  // last, instead of one replacing the other.
  it("opens a second window for a second node", () => {
    const { container } = render(<GraphFullView />);
    click(container, "hover-a");
    act(() => vi.advanceTimersByTime(previewOpenDelay + 10));
    floating.windows = [{ id: "win-1", content: { kind: "note", noteID: "a" } }];

    click(container, "hover-b");
    act(() => vi.advanceTimersByTime(previewOpenDelay + 10));
    expect(floating.open).toHaveBeenCalledTimes(2);
    expect(floating.open).toHaveBeenLastCalledWith(
      { kind: "note", noteID: "b" },
      expect.anything(),
      false,
      expect.objectContaining({ transient: true }),
    );
  });

  it("navigates to the note on click", () => {
    const { container } = render(<GraphFullView />);
    click(container, "select-a");
    expect(navigate).toHaveBeenCalledWith({ to: "/notes/$noteId", params: { noteId: "a" } });
  });
});

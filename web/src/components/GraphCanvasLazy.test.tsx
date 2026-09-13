import { act, render, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { GraphCanvas } from "./GraphCanvasLazy";

const graph = {
  center_id: "center",
  nodes: [
    { note_id: "center", file_kind: "note", title: "Center", center: true },
    { note_id: "neighbor", file_kind: "note", title: "Neighbor" },
  ],
  edges: [{ source_id: "center", target_id: "neighbor" }],
};

// The lazy canvas must fill its parent exactly like the canvas used to when it was the direct
// flex item: one stable host box in both the waiting and the loaded state, with the canvas
// inside it. A bare wrapper div between the flex parent and the canvas collapses the canvas
// (the SSG note-aside graph rendered at 1x1).
describe("GraphCanvasLazy host box", () => {
  let intersectionCallback: ((entries: { isIntersecting: boolean }[]) => void) | undefined;

  beforeEach(() => {
    intersectionCallback = undefined;
    vi.stubGlobal(
      "IntersectionObserver",
      class {
        constructor(callback: (entries: { isIntersecting: boolean }[]) => void) {
          intersectionCallback = callback;
        }
        observe() {}
        disconnect() {}
      },
    );
    vi.stubGlobal(
      "ResizeObserver",
      class {
        observe() {}
        disconnect() {}
      },
    );
    vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue({
      clearRect: vi.fn(),
      save: vi.fn(),
      translate: vi.fn(),
      scale: vi.fn(),
      beginPath: vi.fn(),
      moveTo: vi.fn(),
      lineTo: vi.fn(),
      stroke: vi.fn(),
      arc: vi.fn(),
      fill: vi.fn(),
      restore: vi.fn(),
      setTransform: vi.fn(),
      measureText: vi.fn(() => ({ width: 40 })),
      fillText: vi.fn(),
      rect: vi.fn(),
    } as unknown as CanvasRenderingContext2D);
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it("keeps one host box and loads the canvas inside it once visible", async () => {
    const { container } = render(<GraphCanvas graph={graph} onSelect={vi.fn()} resetToken={0} />);
    const host = container.querySelector(".graph-canvas-host");
    expect(host).not.toBeNull();
    // Still waiting for the viewport: the host reserves the space, no canvas yet.
    expect(container.querySelector("canvas")).toBeNull();

    act(() => {
      intersectionCallback?.([{ isIntersecting: true }]);
    });

    await waitFor(() => {
      expect(container.querySelector("canvas.graph-canvas")).not.toBeNull();
    });
    // The observer target never swaps: the loaded canvas arrives inside the same host.
    expect(container.querySelector(".graph-canvas-host")).toBe(host);
    expect(host!.contains(container.querySelector("canvas"))).toBe(true);
    // The canvas is the host's direct flex child — the 1x1 regression was a bare wrapper div sitting
    // between the sized host and the canvas, which stopped the canvas's flex sizing from applying
    // and collapsed the SSG note-aside graph. A wrapper anywhere in between would fail this.
    expect(container.querySelector("canvas")!.parentElement).toBe(host);
  });

  it("renders nothing but the host while off-screen", () => {
    const { container } = render(<GraphCanvas graph={graph} onSelect={vi.fn()} resetToken={0} />);
    expect(container.querySelector(".graph-canvas-host")).not.toBeNull();
    expect(container.querySelector("canvas")).toBeNull();
  });
});

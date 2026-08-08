import { render, waitFor } from "@testing-library/react";
import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";
import { GraphCanvas } from "./GraphCanvas";

interface CanvasMock {
  globalAlpha: number;
  clearRect: ReturnType<typeof vi.fn>;
  save: ReturnType<typeof vi.fn>;
  translate: ReturnType<typeof vi.fn>;
  scale: ReturnType<typeof vi.fn>;
  beginPath: ReturnType<typeof vi.fn>;
  moveTo: ReturnType<typeof vi.fn>;
  lineTo: ReturnType<typeof vi.fn>;
  stroke: ReturnType<typeof vi.fn>;
  arc: ReturnType<typeof vi.fn>;
  fill: ReturnType<typeof vi.fn>;
  restore: ReturnType<typeof vi.fn>;
  measureText: ReturnType<typeof vi.fn>;
  fillText: ReturnType<typeof vi.fn>;
  rect: ReturnType<typeof vi.fn>;
}

function makeCanvasContext(): CanvasMock {
  return {
    globalAlpha: 1,
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
    measureText: vi.fn(() => ({ width: 40 })),
    fillText: vi.fn(),
    rect: vi.fn(),
  };
}

const graph = {
  center_id: "center",
  nodes: [
    { note_id: "center", file_kind: "note", title: "Center", center: true },
    { note_id: "neighbor", file_kind: "note", title: "Neighbor" },
  ],
  edges: [{ source_id: "center", target_id: "neighbor" }],
};

let context: CanvasMock;
const fillAlphas: number[] = [];

beforeAll(() => {
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      disconnect() {}
    },
  );
  context = makeCanvasContext();
  context.fill.mockImplementation(function (this: CanvasMock) {
    fillAlphas.push(this.globalAlpha);
  });
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(
    context as unknown as CanvasRenderingContext2D,
  );
});

afterAll(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("GraphCanvas node painting", () => {
  it("paints node interiors opaquely so edges do not show through", async () => {
    fillAlphas.length = 0;
    render(<GraphCanvas graph={graph} onSelect={vi.fn()} resetToken={0} decorative />);

    await waitFor(() => expect(fillAlphas.length).toBeGreaterThanOrEqual(2));

    // Decorative mode omits labels, so every fill is a node interior. The last draw pass is the
    // settled graph, and both nodes must cover the edge beneath them completely.
    expect(fillAlphas.slice(-2)).toEqual([1, 1]);
  });
});

import { act, fireEvent, render } from "@testing-library/react";
import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";
import { GraphOverviewCanvas } from "./GraphOverviewCanvas";

interface ContextMock {
  globalAlpha: number;
  fillStyle: string;
  strokeStyle: string;
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
}

function makeContext(): ContextMock {
  return {
    globalAlpha: 1,
    fillStyle: "",
    strokeStyle: "",
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
  };
}

let context: ContextMock;
let fireResize: ((entries: Array<{ contentRect: { width: number; height: number } }>) => void) | undefined;

beforeAll(() => {
  vi.stubGlobal(
    "ResizeObserver",
    class {
      constructor(callback: (entries: Array<{ contentRect: { width: number; height: number } }>) => void) {
        fireResize = callback;
      }
      observe() {}
      disconnect() {}
    },
  );
  context = makeContext();
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(
    context as unknown as CanvasRenderingContext2D,
  );
});

afterAll(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

// One node at the layout origin: the fit centres it on the canvas, so its screen position is
// predictable without reaching into the layout module.
const soloGraph = {
  center_id: "solo",
  nodes: [{ note_id: "solo", file_kind: "note", title: "Solo" }],
  edges: [],
};

function renderOverview(graph = soloGraph, resetToken = 0) {
  const onSelect = vi.fn();
  const utils = render(<GraphOverviewCanvas graph={graph} onSelect={onSelect} resetToken={resetToken} />);
  const canvas = utils.container.querySelector("canvas")!;
  return { onSelect, canvas, ...utils };
}

function showSize(width: number, height: number) {
  act(() => fireResize?.([{ contentRect: { width, height } }]));
}

describe("GraphOverviewCanvas", () => {
  it("draws once mounted, edges under nodes", () => {
    const { canvas } = renderOverview();
    expect(canvas).toBeInTheDocument();
    expect(context.clearRect).toHaveBeenCalled();
    // The single draw pass orders edges before nodes; with no edges only node arcs appear.
    expect(context.arc).toHaveBeenCalled();
    expect(context.moveTo).not.toHaveBeenCalled();
  });

  it("redraws when the canvas is resized", () => {
    renderOverview();
    const drawsBefore = context.clearRect.mock.calls.length;
    showSize(400, 300);
    expect(context.clearRect.mock.calls.length).toBeGreaterThan(drawsBefore);
  });

  it("redraws when the reset control advances the token", () => {
    const utils = renderOverview();
    const drawsBefore = context.clearRect.mock.calls.length;
    utils.rerender(
      <GraphOverviewCanvas graph={soloGraph} onSelect={vi.fn()} resetToken={1} />,
    );
    expect(context.clearRect.mock.calls.length).toBeGreaterThan(drawsBefore);
  });

  it("selects the node under a press that did not drag", () => {
    const { canvas, onSelect } = renderOverview();
    showSize(400, 300);
    onSelect.mockClear();
    // The fitted single node sits at the centre of the 400x300 canvas.
    fireEvent.pointerDown(canvas, { clientX: 200, clientY: 150 });
    fireEvent.pointerUp(canvas, { clientX: 200, clientY: 150 });
    expect(onSelect).toHaveBeenCalledWith("solo");
  });

  it("pans instead of selecting when a press drags away", () => {
    const { canvas, onSelect } = renderOverview();
    showSize(400, 300);
    onSelect.mockClear();
    // Each draw translates by width/2 + view.x, so the delta between consecutive transforms is the
    // pan itself.
    const lastTranslateX = () => context.translate.mock.calls.at(-1)![0] as number;
    const before = lastTranslateX();
    fireEvent.pointerDown(canvas, { clientX: 200, clientY: 150 });
    fireEvent.pointerMove(canvas, { clientX: 320, clientY: 150 });
    fireEvent.pointerUp(canvas, { clientX: 320, clientY: 150 });
    expect(onSelect).not.toHaveBeenCalled();
    expect(lastTranslateX()).toBe(before + 120);
  });

  it("ignores presses on empty space far from any node", () => {
    const { canvas, onSelect } = renderOverview();
    showSize(400, 300);
    onSelect.mockClear();
    fireEvent.pointerDown(canvas, { clientX: 5, clientY: 5 });
    fireEvent.pointerUp(canvas, { clientX: 5, clientY: 5 });
    expect(onSelect).not.toHaveBeenCalled();
  });

  it("points at a hovered node and clears on leave", () => {
    const { canvas } = renderOverview();
    showSize(400, 300);
    fireEvent.pointerMove(canvas, { clientX: 200, clientY: 150 });
    expect(canvas.style.cursor).toBe("pointer");
    fireEvent.pointerLeave(canvas);
    expect(canvas.style.cursor).toBe("");
  });
});

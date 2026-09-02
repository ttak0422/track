import { fireEvent, render, waitFor } from "@testing-library/react";
import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";
import { GraphCanvas } from "./GraphCanvas";

interface CanvasMock {
  globalAlpha: number;
  fillStyle: string;
  strokeStyle: string;
  font: string;
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
  setTransform: ReturnType<typeof vi.fn>;
  measureText: ReturnType<typeof vi.fn>;
  fillText: ReturnType<typeof vi.fn>;
  rect: ReturnType<typeof vi.fn>;
}

function makeCanvasContext(): CanvasMock {
  return {
    globalAlpha: 1,
    fillStyle: "",
    strokeStyle: "",
    font: "",
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

const longTitleGraph = {
  center_id: "center",
  nodes: [
    {
      note_id: "center",
      file_kind: "note",
      title: "A title that is deliberately much longer than twenty-four characters",
      center: true,
    },
  ],
  edges: [],
};

let context: CanvasMock;
const fillAlphas: number[] = [];
const fillStyles: string[] = [];
let themeChange: (() => void) | undefined;

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
    fillStyles.push(this.fillStyle);
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

  it("redraws with the current CSS colors when the system theme changes", async () => {
    document.documentElement.style.setProperty("--line-node", "#old");
    const media = {
      addEventListener: vi.fn((_type: string, listener: () => void) => {
        themeChange = listener;
      }),
      removeEventListener: vi.fn(),
    };
    vi.stubGlobal("matchMedia", vi.fn(() => media));

    render(<GraphCanvas graph={graph} onSelect={vi.fn()} resetToken={0} decorative />);
    await waitFor(() => expect(context.clearRect).toHaveBeenCalled());
    const drawsBeforeThemeChange = context.clearRect.mock.calls.length;

    document.documentElement.style.setProperty("--line-node", "#new");
    themeChange?.();

    await waitFor(() => expect(context.clearRect.mock.calls.length).toBeGreaterThan(drawsBeforeThemeChange));
    expect(context.strokeStyle).toBe("#new");

    document.documentElement.style.removeProperty("--line-node");
    vi.unstubAllGlobals();
  });

  it("shows the full title for the hovered node and measures that title for its backdrop", async () => {
    vi.stubGlobal(
      "ResizeObserver",
      class {
        observe() {}
        disconnect() {}
      },
    );
    const { container } = render(
      <GraphCanvas graph={longTitleGraph} onSelect={vi.fn()} resetToken={0} focusNodeID="center" />,
    );
    const canvas = container.querySelector("canvas")!;
    context.measureText.mockClear();

    fireEvent.pointerMove(canvas, { clientX: 0, clientY: 0 });

    await waitFor(() => {
      expect(context.measureText).toHaveBeenCalledWith(longTitleGraph.nodes[0].title);
    });
  });

  // Labels are rasterized under a devicePixelRatio-only transform at a fixed CSS-pixel font, never
  // through the zoom CTM with an inversely-scaled font — the zoom path hands the rasterizer a
  // fractional scale (or a font smaller than the pixels it lands on, zoomed in), which reads as rough
  // text. The backing store is already ratio-sized; this pins the draw pass to match it.
  it("draws labels at a fixed CSS-pixel font under the devicePixelRatio-only transform", async () => {
    Object.defineProperty(window, "devicePixelRatio", { value: 2, configurable: true });
    context.setTransform.mockClear();
    context.fillText.mockClear();

    render(<GraphCanvas graph={graph} onSelect={vi.fn()} resetToken={0} focusNodeID="center" />);
    await waitFor(() => expect(context.fillText).toHaveBeenCalled());

    // The label pass drops the zoom transform for the pure ratio scale (device px = css px × ratio).
    expect(context.setTransform).toHaveBeenCalledWith(2, 0, 0, 2, 0, 0);
    // The font is a fixed 13 CSS px, not floor(13·ratio / view.scale) in user space.
    expect(String(context.font)).toMatch(/^13px /);

    delete (window as { devicePixelRatio?: number }).devicePixelRatio;
  });

  // A frame frozen mid-hover has to answer both "what am I pointing at" and "where am I", so the
  // highlight is ink and the salient stays with the centre (design.md, Sidebar).
  it("highlights in ink and leaves the salient on the centre node", async () => {
    const root = document.documentElement.style;
    root.setProperty("--mark", "#mark");
    root.setProperty("--text", "#ink");

    const { container } = render(
      <GraphCanvas graph={graph} onSelect={vi.fn()} resetToken={0} focusNodeID="center" />,
    );
    const canvas = container.querySelector("canvas")!;
    fillStyles.length = 0;
    fireEvent.pointerMove(canvas, { clientX: 0, clientY: 0 });

    await waitFor(() => expect(fillStyles).toContain("#ink"));
    expect(fillStyles).toContain("#mark");

    root.removeProperty("--mark");
    root.removeProperty("--text");
  });
});

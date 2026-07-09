import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { computeCollapsedFit, computeFit, MermaidDiagram } from "./MermaidDiagram";

// jsdom does not implement pointer capture; drag relies on it, so stub it to a no-op.
beforeAll(() => {
  Element.prototype.setPointerCapture = () => {};
  Element.prototype.releasePointerCapture = () => {};
});

vi.mock("mermaid", () => ({
  default: {
    initialize: vi.fn(),
    render: vi.fn(async () => ({ svg: "<svg><text>Diagram</text></svg>" })),
  },
}));

describe("MermaidDiagram", () => {
  afterEach(() => {
    vi.clearAllMocks();
  });

  it("renders generated SVG", async () => {
    const { container } = render(<MermaidDiagram text={"graph TD\nA-->B"} />);
    expect(screen.getByText("Rendering diagram...")).toBeInTheDocument();
    await waitFor(() => expect(container.querySelector("svg")).toBeInTheDocument());
    expect(screen.getByRole("img", { name: "Mermaid diagram" })).toBeInTheDocument();
  });

  it("pans on drag and returns to origin on reset", async () => {
    const { container } = render(<MermaidDiagram text={"graph TD\nA-->B"} />);
    await waitFor(() => expect(container.querySelector("svg")).toBeInTheDocument());
    const viewport = container.querySelector(".mermaid-viewport") as HTMLElement;
    const pan = screen.getByRole("img", { name: "Mermaid diagram" });

    fireEvent.pointerDown(viewport, { pointerId: 1, clientX: 0, clientY: 0 });
    fireEvent.pointerMove(viewport, { pointerId: 1, clientX: 40, clientY: 25 });
    expect(pan.style.transform).toBe("translate(40px, 25px) scale(1)");

    fireEvent.click(screen.getByRole("button", { name: "Reset diagram view" }));
    expect(pan.style.transform).toBe("translate(0px, 0px) scale(1)");
  });

  it("zooms only on Shift/ctrl wheel; a plain wheel keeps scrolling the page", async () => {
    const { container } = render(<MermaidDiagram text={"graph TD\nA-->B"} />);
    await waitFor(() => expect(container.querySelector("svg")).toBeInTheDocument());
    const viewport = container.querySelector(".mermaid-viewport") as HTMLElement;
    const pan = screen.getByRole("img", { name: "Mermaid diagram" });
    const scaleOf = () => Number(pan.style.transform.match(/scale\(([^)]+)\)/)?.[1]);

    fireEvent.wheel(viewport, { deltaY: -240 });
    expect(scaleOf()).toBe(1);

    fireEvent.wheel(viewport, { deltaY: -240, shiftKey: true });
    expect(scaleOf()).toBeGreaterThan(1);
    const shifted = scaleOf();

    // Shift+wheel arrives on the horizontal axis in some browsers; the delta still zooms.
    fireEvent.wheel(viewport, { deltaX: -240, deltaY: 0, shiftKey: true });
    expect(scaleOf()).toBeGreaterThan(shifted);
  });

  it("pins the SVG to its viewBox size so mermaid's width=100% cannot squish it", async () => {
    const { default: mermaid } = await import("mermaid");
    vi.mocked(mermaid.render).mockResolvedValueOnce({
      svg: '<svg viewBox="0 0 866 217" width="100%" style="max-width: 866px;"></svg>',
    } as Awaited<ReturnType<typeof mermaid.render>>);
    const { container } = render(<MermaidDiagram text={"graph LR\nA-->B"} />);
    await waitFor(() => expect(container.querySelector("svg")).toBeInTheDocument());
    const svg = container.querySelector("svg") as SVGSVGElement;
    expect(svg.style.width).toBe("866px");
    expect(svg.style.height).toBe("217px");
    expect(svg.style.maxWidth).toBe("none");
  });

  it("copies the diagram source, not the rendered SVG", async () => {
    const writeText = vi.fn(async () => {});
    Object.assign(navigator, { clipboard: { writeText } });
    const source = "graph TD\nA-->B";
    const { container } = render(<MermaidDiagram text={source} />);
    await waitFor(() => expect(container.querySelector("svg")).toBeInTheDocument());

    fireEvent.click(screen.getByRole("button", { name: "Copy source" }));
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(source));
    expect(await screen.findByRole("button", { name: "Copied" })).toBeInTheDocument();
  });

  it("zooms in and out with the control buttons", async () => {
    const { container } = render(<MermaidDiagram text={"graph TD\nA-->B"} />);
    await waitFor(() => expect(container.querySelector("svg")).toBeInTheDocument());
    const pan = screen.getByRole("img", { name: "Mermaid diagram" });
    const scaleOf = () => Number(pan.style.transform.match(/scale\(([^)]+)\)/)?.[1]);

    fireEvent.click(screen.getByRole("button", { name: "Zoom in" }));
    expect(scaleOf()).toBeCloseTo(1.3);

    fireEvent.click(screen.getByRole("button", { name: "Zoom out" }));
    expect(scaleOf()).toBeCloseTo(1);
  });
});

describe("computeFit", () => {
  it("shrinks a wide diagram to 80% width and centers it", () => {
    const { transform, height } = computeFit(1000, 400, 500);
    expect(transform.scale).toBeCloseTo(0.4); // 500 * 0.8 / 1000
    expect(transform.x).toBeCloseTo(50); // (500 - 1000 * 0.4) / 2
    expect(height).toBeCloseTo(160); // 400 * 0.4
  });

  it("keeps a small diagram at the ideal scale instead of inflating it to fill the width", () => {
    const { transform } = computeFit(100, 60, 500);
    expect(transform.scale).toBe(1);
    expect(transform.x).toBeCloseTo(200); // (500 - 100 * 1) / 2
  });

  it("scales toward a larger article font, still capped by the viewport width", () => {
    const ideal = computeFit(100, 60, 500, 1.25);
    expect(ideal.transform.scale).toBeCloseTo(1.25);

    const capped = computeFit(1000, 400, 500, 1.25);
    expect(capped.transform.scale).toBeCloseTo(0.4); // width cap binds before the ideal scale
  });
});

describe("computeCollapsedFit", () => {
  it("fits a tall diagram whole inside the collapsed height", () => {
    // 400x2200 at 500 wide: the width fit (scale 1) would be 2200 tall; collapsed caps at 220.
    const { transform, height } = computeCollapsedFit(400, 2200, 500);
    expect(transform.scale).toBeCloseTo(0.1); // 220 / 2200
    expect(height).toBeCloseTo(220);
    expect(transform.x).toBeCloseTo((500 - 400 * 0.1) / 2);
  });

  it("never scales wider than the normal width fit", () => {
    // A short-and-wide diagram: the height cap is not the binding constraint.
    const collapsed = computeCollapsedFit(1000, 100, 500);
    expect(collapsed.transform.scale).toBeCloseTo(0.4); // same as computeFit
    expect(collapsed.height).toBeCloseTo(40);
  });
});

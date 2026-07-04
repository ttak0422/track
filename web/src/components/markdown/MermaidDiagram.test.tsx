import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { computeFit, MermaidDiagram } from "./MermaidDiagram";

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

  it("enlarges a small diagram to 80% width", () => {
    const { transform } = computeFit(100, 60, 500);
    expect(transform.scale).toBeCloseTo(4); // 500 * 0.8 / 100
    expect(transform.x).toBeCloseTo(50); // (500 - 100 * 4) / 2
  });
});

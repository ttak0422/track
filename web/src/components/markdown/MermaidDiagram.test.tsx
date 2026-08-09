import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import {
  computeCollapsedFit,
  computeFit,
  DiagramFrame,
  isDarkColor,
  MermaidDiagram,
  mermaidConfig,
} from "./MermaidDiagram";

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

describe("DiagramFrame tall-diagram preview", () => {
  it("keeps text at the normal fit scale and reveals the rest through an explicit control", () => {
    const clientWidth = vi
      .spyOn(HTMLElement.prototype, "clientWidth", "get")
      .mockReturnValue(500);
    const offsetWidth = vi
      .spyOn(HTMLElement.prototype, "offsetWidth", "get")
      .mockImplementation(function (this: HTMLElement) {
        return this.classList.contains("mermaid-pan") ? 400 : 0;
      });
    const offsetHeight = vi
      .spyOn(HTMLElement.prototype, "offsetHeight", "get")
      .mockImplementation(function (this: HTMLElement) {
        return this.classList.contains("mermaid-pan") ? 2200 : 0;
      });

    const { container } = render(
      <DiagramFrame
        state={{ status: "ready", svg: '<svg viewBox="0 0 400 2200"></svg>' }}
        source="graph TD"
        sourceLang="mermaid"
        label="Tall diagram"
      />,
    );

    const viewport = container.querySelector(".mermaid-viewport") as HTMLElement;
    const pan = screen.getByRole("img", { name: "Tall diagram" });
    expect(viewport).toHaveAttribute("data-collapsed");
    expect(viewport.style.height).toBe("320px");
    expect(pan.style.transform).toBe("translate(50px, 0px) scale(1)");
    expect(container.querySelector(".mermaid-continuation")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Open diagram in popup" })).toBeInTheDocument();

    const expand = screen.getByRole("button", { name: "Expand diagram" });
    expect(expand).toHaveTextContent("Show full diagram");
    fireEvent.click(expand);

    expect(viewport).not.toHaveAttribute("data-collapsed");
    expect(viewport.style.height).toBe("2200px");
    expect(container.querySelector(".mermaid-continuation")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Zoom in" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Collapse diagram" })).toBeInTheDocument();

    clientWidth.mockRestore();
    offsetWidth.mockRestore();
    offsetHeight.mockRestore();
  });
});

describe("DiagramFrame wide-diagram clipping", () => {
  // Mounts a panW×panH diagram in a 500px viewport and returns the mounted handles plus the
  // layout-mock teardown.
  function setupWide(panW = 2000, panH = 300) {
    const clientWidth = vi
      .spyOn(HTMLElement.prototype, "clientWidth", "get")
      .mockReturnValue(500);
    const offsetWidth = vi
      .spyOn(HTMLElement.prototype, "offsetWidth", "get")
      .mockImplementation(function (this: HTMLElement) {
        return this.classList.contains("mermaid-pan") ? panW : 0;
      });
    const offsetHeight = vi
      .spyOn(HTMLElement.prototype, "offsetHeight", "get")
      .mockImplementation(function (this: HTMLElement) {
        return this.classList.contains("mermaid-pan") ? panH : 0;
      });
    const { container } = render(
      <DiagramFrame
        state={{ status: "ready", svg: `<svg viewBox="0 0 ${panW} ${panH}"></svg>` }}
        source="graph LR"
        sourceLang="mermaid"
        label="Wide diagram"
      />,
    );
    const viewport = container.querySelector(".mermaid-viewport") as HTMLElement;
    const fade = (side: "left" | "right") =>
      container.querySelector(`.mermaid-continuation-${side}`);
    const restore = () => {
      clientWidth.mockRestore();
      offsetWidth.mockRestore();
      offsetHeight.mockRestore();
    };
    return { container, viewport, fade, restore };
  }

  it("keeps readable text, clips at the edge, and fades the clipped side", () => {
    const { viewport, fade, restore } = setupWide();

    const pan = screen.getByRole("img", { name: "Wide diagram" });
    expect(viewport).not.toHaveAttribute("data-collapsed");
    expect(screen.queryByRole("button", { name: "Collapse diagram" })).not.toBeInTheDocument();
    expect(viewport.style.height).toBe("225px"); // 300 * 0.75: floored, not shrunk to fit
    expect(pan.style.transform).toBe("translate(0px, 0px) scale(0.75)");
    expect(fade("right")).toBeInTheDocument();
    expect(fade("left")).not.toBeInTheDocument();

    // Panning right reveals the left edge fade; the right side is still clipped.
    fireEvent.pointerDown(viewport, { pointerId: 1, clientX: 0, clientY: 0 });
    fireEvent.pointerMove(viewport, { pointerId: 1, clientX: -100, clientY: 0 });
    expect(fade("left")).toBeInTheDocument();
    expect(fade("right")).toBeInTheDocument();

    restore();
  });

  it("opens the full diagram in a popup", () => {
    const { container, restore } = setupWide();

    fireEvent.click(screen.getByRole("button", { name: "Open diagram in popup" }));
    const dialog = container.querySelector("dialog.diagram-lightbox") as HTMLDialogElement;
    expect(dialog).toBeInTheDocument();
    expect(dialog.open).toBe(true);
    expect(dialog.querySelector("svg")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Close diagram popup" }));
    expect(container.querySelector("dialog.diagram-lightbox")).not.toBeInTheDocument();
    restore();
  });

  it("pans on a horizontal wheel while clipped; a vertical wheel keeps scrolling the page", () => {
    const { viewport, fade, restore } = setupWide();
    const pan = screen.getByRole("img", { name: "Wide diagram" });

    // fireEvent returns false when the handler consumed (preventDefaulted) the event.
    expect(fireEvent.wheel(viewport, { deltaX: 120, deltaY: 4 })).toBe(false);
    expect(pan.style.transform).toBe("translate(-120px, 0px) scale(0.75)");

    expect(fireEvent.wheel(viewport, { deltaY: 120 })).toBe(true);
    expect(pan.style.transform).toBe("translate(-120px, 0px) scale(0.75)");

    // The pan clamps to the diagram's far end, like a native scroller, and the fades follow.
    fireEvent.wheel(viewport, { deltaX: 5000 });
    expect(pan.style.transform).toBe("translate(-1000px, 0px) scale(0.75)"); // 500 - 2000 * 0.75
    expect(fade("right")).not.toBeInTheDocument();
    expect(fade("left")).toBeInTheDocument();

    // ...and back to the start; a tick at an end is left unconsumed, so an edge swipe falls
    // through to the browser instead of dying on a diagram that cannot move further.
    fireEvent.wheel(viewport, { deltaX: -5000 });
    expect(pan.style.transform).toBe("translate(0px, 0px) scale(0.75)");
    expect(fireEvent.wheel(viewport, { deltaX: -100 })).toBe(true);

    restore();
  });

  it("leaves a horizontal wheel alone when the diagram fits", () => {
    const { viewport, restore } = setupWide(400, 300);
    const pan = screen.getByRole("img", { name: "Wide diagram" });
    expect(pan.style.transform).toBe("translate(50px, 0px) scale(1)");

    expect(fireEvent.wheel(viewport, { deltaX: 120, deltaY: 4 })).toBe(true);
    expect(pan.style.transform).toBe("translate(50px, 0px) scale(1)");

    restore();
  });

  it("keeps the inert collapsed preview free of side fades until expanded", () => {
    const { viewport, fade, restore } = setupWide(2000, 2200);
    expect(viewport).toHaveAttribute("data-collapsed");
    expect(fade("right")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Expand diagram" }));
    expect(fade("right")).toBeInTheDocument();

    restore();
  });
});

describe("mermaidConfig dark mode", () => {
  it("classifies theme surfaces by luminance", () => {
    expect(isDarkColor("#141618")).toBe(true); // dark theme --bg
    expect(isDarkColor("#fbfaf8")).toBe(false); // light theme --bg
    // What getPropertyValue actually returns for a registered custom property (styles.css).
    expect(isDarkColor("rgb(20, 22, 24)")).toBe(true);
    expect(isDarkColor("rgb(251, 250, 248)")).toBe(false);
    expect(isDarkColor("not-a-color")).toBe(false); // unparseable: keep light derivations
  });

  it("passes darkMode and textColor to the base theme", () => {
    const variables = mermaidConfig().themeVariables as Record<string, unknown>;
    expect(variables.darkMode).toBe(false); // jsdom resolves no tokens: light fallbacks
    expect(variables.textColor).toBe("#1a1a18");
  });
});

describe("computeFit", () => {
  it("shrinks a slightly wide diagram to 80% width and centers it", () => {
    const { transform, height } = computeFit(500, 400, 500);
    expect(transform.scale).toBeCloseTo(0.8); // 500 * 0.8 / 500, above the readability floor
    expect(transform.x).toBeCloseTo(50); // (500 - 500 * 0.8) / 2
    expect(height).toBeCloseTo(320); // 400 * 0.8
  });

  it("keeps a small diagram at the ideal scale instead of inflating it to fill the width", () => {
    const { transform } = computeFit(100, 60, 500);
    expect(transform.scale).toBe(1);
    expect(transform.x).toBeCloseTo(200); // (500 - 100 * 1) / 2
  });

  it("scales toward a larger article font, still capped by the viewport width", () => {
    const ideal = computeFit(100, 60, 500, 1.25);
    expect(ideal.transform.scale).toBeCloseTo(1.25);

    const capped = computeFit(400, 300, 500, 1.25);
    expect(capped.transform.scale).toBeCloseTo(1); // width cap binds before the ideal scale
  });

  it("stops shrinking a wide diagram at the readability floor and left-aligns the clipped fit", () => {
    const { transform, height } = computeFit(2000, 300, 500);
    expect(transform.scale).toBeCloseTo(0.75); // floor, not 500 * 0.8 / 2000 = 0.2
    expect(transform.x).toBe(0); // clipped: show the start, not a centered middle slice
    expect(height).toBeCloseTo(225); // 300 * 0.75

    // The floor follows the article font: text never drops below 75% of the surrounding size.
    expect(computeFit(2000, 300, 500, 1.25).transform.scale).toBeCloseTo(0.9375);
  });
});

describe("computeCollapsedFit", () => {
  it("keeps a tall diagram readable and clips only the viewport", () => {
    // 400x2200 at 500 wide fits at scale 1; the collapsed preview shows its first 320px.
    const { transform, height } = computeCollapsedFit(400, 2200, 500);
    expect(transform.scale).toBeCloseTo(1);
    expect(height).toBeCloseTo(320);
    expect(transform.x).toBeCloseTo(50);
  });

  it("never scales wider than the normal width fit", () => {
    // A short-and-wide diagram: the height cap is not the binding constraint.
    const collapsed = computeCollapsedFit(1000, 100, 500);
    expect(collapsed.transform.scale).toBeCloseTo(0.75); // same floored scale as computeFit
    expect(collapsed.height).toBeCloseTo(75);
  });
});

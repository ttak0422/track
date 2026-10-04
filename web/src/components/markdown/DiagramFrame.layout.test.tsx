import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { DiagramFrame } from "./MermaidDiagram";

// jsdom has no layout engine: supply the measured boxes, including an asymmetric reader, a real
// scrollbar allowance, and an aside's padded left edge. The frame must use those boxes, not 100vw.
let geometry: { readerWidth: number; frameLeft: number; frameWidth: number; asideLeft: number; naturalWidth: number; naturalHeight: number };
let resizeCallbacks: (() => void)[];
const bounds = (left: number, width: number, height = 100): DOMRect =>
  ({ left, right: left + width, width, height, top: 0, bottom: height, x: left, y: 0, toJSON() {} });

beforeEach(() => {
  geometry = { readerWidth: 1500, frameLeft: 280, frameWidth: 848, asideLeft: 1196, naturalWidth: 2000, naturalHeight: 800 };
  resizeCallbacks = [];
  vi.stubGlobal("ResizeObserver", class {
    constructor(callback: () => void) { resizeCallbacks.push(callback); }
    observe() {}
    disconnect() {}
  });
  vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockImplementation(function (this: HTMLElement) {
    if (this.classList.contains("reader")) return geometry.readerWidth;
    return parseFloat(this.style.width) || geometry.frameWidth;
  });
  vi.spyOn(HTMLElement.prototype, "offsetWidth", "get").mockImplementation(function (this: HTMLElement) {
    return this.classList.contains("mermaid-pan") ? geometry.naturalWidth : 0;
  });
  vi.spyOn(HTMLElement.prototype, "offsetHeight", "get").mockImplementation(function (this: HTMLElement) {
    return this.classList.contains("mermaid-pan") ? geometry.naturalHeight : 0;
  });
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function (this: HTMLElement) {
    if (this.classList.contains("reader")) return bounds(0, geometry.readerWidth + 16);
    if (this.classList.contains("note-aside")) return bounds(geometry.asideLeft, 300);
    if (this.classList.contains("mermaid-viewport")) {
      return bounds(geometry.frameLeft + (parseFloat(this.style.marginLeft) || 0), this.clientWidth, parseFloat(this.style.height));
    }
    return bounds(geometry.frameLeft, geometry.frameWidth);
  });
  Element.prototype.setPointerCapture = () => {};
  Element.prototype.releasePointerCapture = () => {};
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  localStorage.clear();
});

function Surface({ stacked = false, split = false, included = false, mobile = false } = {}) {
  const diagram = <DiagramFrame state={{ status: "ready", svg: `<svg viewBox="0 0 ${geometry.naturalWidth} ${geometry.naturalHeight}"></svg>` }} source="a -> b" sourceLang="d2" label="Test diagram" />;
  return <div className="reader" style={{ paddingLeft: mobile ? 12 : 64, paddingRight: mobile ? 12 : 32 }}>
    <article className="note-reader"><div className="note-layout" style={{ flexDirection: stacked ? "column" : "row" }}>
      <div className="note-main"><div className="note-editor">
        {split && <textarea aria-label="Editor" />}
        <section className="note-preview" style={{ overflowX: split ? "auto" : "visible" }}>
          <div className="markdown-view">
            {included ? <div className="note-include"><div className="markdown-view">{diagram}</div></div> : diagram}
          </div>
        </section>
      </div></div>
      <aside className="note-aside" />
    </div></article>
  </div>;
}

function elements(container: HTMLElement) {
  const viewport = container.querySelector(".mermaid-viewport") as HTMLElement;
  const pan = screen.getByRole("img", { name: "Test diagram" });
  const transform = () => {
    const match = pan.style.transform.match(/translate\(([-\d.e]+)px, ([-\d.e]+)px\) scale\(([-\d.e]+)\)/)!;
    return { x: Number(match[1]), y: Number(match[2]), scale: Number(match[3]) };
  };
  const expectFit = () => {
    const { x, scale } = transform();
    expect(x).toBeGreaterThanOrEqual(-0.001);
    expect(x + geometry.naturalWidth * scale).toBeLessThanOrEqual(viewport.clientWidth + 0.001);
  };
  return { viewport, pan, transform, expectFit };
}
const resize = () => act(() => resizeCallbacks.forEach(callback => callback()));

describe("DiagramFrame available reading width", () => {
  it("bleeds beyond prose but ends before the docked sidebar", () => {
    const { container } = render(<Surface />);
    const { viewport, expectFit } = elements(container);
    expect(viewport.style.width).toBe("1132px"); // 1196 - 64; never the 1516px window
    expect(viewport.style.marginLeft).toBe("-216px");
    expect(viewport.getBoundingClientRect().right).toBe(1196);
    expectFit();
  });

  it("refits between docked, stacked, narrow mobile, and full content widths", () => {
    const view = render(<Surface />);
    const { viewport, expectFit } = elements(view.container);
    geometry.readerWidth = 1099;
    geometry.frameLeft = 80;
    geometry.frameWidth = 880;
    view.rerender(<Surface stacked />);
    resize();
    expect(viewport.style.width).toBe("1003px");
    expectFit();

    geometry.readerWidth = 390;
    geometry.frameLeft = 28;
    geometry.frameWidth = 334;
    view.rerender(<Surface stacked mobile />);
    resize();
    expect(viewport.style.width).toBe("366px");
    expectFit();

    geometry.readerWidth = 1800;
    geometry.frameLeft = 80;
    geometry.frameWidth = 1250;
    geometry.asideLeft = 1390;
    view.rerender(<Surface />);
    resize();
    expect(viewport.style.width).toBe("1326px");
    expectFit();
  });

  it("tracks an unchanged-width frame moving after a content-width setting", () => {
    geometry.naturalWidth = 400;
    const { container } = render(<Surface />);
    const { transform, expectFit } = elements(container);
    const before = transform().x;
    geometry.frameLeft -= 120;
    resize();
    expect(transform().x).toBeCloseTo(before - 120);
    expectFit();
  });

  it("preserves user zoom and pan across resize, then Reset uses the new width", () => {
    const { container } = render(<Surface />);
    const { viewport, pan, expectFit } = elements(container);
    fireEvent.click(screen.getByRole("button", { name: "Zoom in" }));
    fireEvent.pointerDown(viewport, { pointerId: 1, clientX: 0, clientY: 0 });
    fireEvent.pointerMove(viewport, { pointerId: 1, clientX: 70, clientY: 30 });
    fireEvent.pointerUp(viewport, { pointerId: 1 });
    const touched = pan.style.transform;
    geometry.asideLeft = 900;
    resize();
    expect(viewport.style.width).toBe("836px");
    expect(pan.style.transform).toBe(touched);
    fireEvent.click(screen.getByRole("button", { name: "Reset diagram view" }));
    expect(pan.style.transform).not.toBe(touched);
    expectFit();
  });

  it.each([{ split: true }, { included: true }])("keeps local preview bounds for %j", (props) => {
    const { container } = render(<Surface {...props} />);
    const { viewport, expectFit } = elements(container);
    expect(viewport.style.width).toBe("");
    expect(viewport.style.marginLeft).toBe("");
    expectFit();
  });

  it("folds at local width, expands safely, and updates fold eligibility on resize", () => {
    geometry.naturalHeight = 2000;
    const { container } = render(<Surface />);
    const { viewport, expectFit } = elements(container);
    fireEvent.click(screen.getByRole("button", { name: "Collapse diagram" }));
    expect(viewport.style.width).toBe("");
    expect(viewport.style.height).toBe("320px");
    expectFit();
    fireEvent.click(screen.getByRole("button", { name: "Expand diagram" }));
    expect(viewport.style.width).toBe("1132px");
    expectFit();
    geometry.asideLeft = 600;
    resize();
    expect(screen.queryByRole("button", { name: "Collapse diagram" })).not.toBeInTheDocument();
    expectFit();
  });

  it("does not make Zoom out enlarge an overview fitted below 0.2", () => {
    geometry.naturalWidth = 20000;
    const { container } = render(<Surface />);
    const { viewport, transform, expectFit } = elements(container);
    const fitted = transform().scale;
    expect(fitted).toBeLessThan(0.2);
    fireEvent.click(screen.getByRole("button", { name: "Zoom out" }));
    expect(transform().scale).toBe(fitted);
    fireEvent.click(screen.getByRole("button", { name: "Zoom in" }));
    expect(transform().scale).toBeCloseTo(fitted * 1.3);
    fireEvent.click(screen.getByRole("button", { name: "Zoom out" }));
    expect(transform().scale).toBeCloseTo(fitted);
    fireEvent.wheel(viewport, { deltaY: 240, ctrlKey: true });
    expect(transform().scale).toBeCloseTo(fitted);
    expectFit();

    // A touched view stays at its old scale across resize. A larger new reset target must not
    // turn Zoom out into Zoom in or prevent zooming back to that original overview.
    geometry.readerWidth = 2100;
    geometry.asideLeft = 1800;
    resize();
    expect(transform().scale).toBeCloseTo(fitted);
    fireEvent.click(screen.getByRole("button", { name: "Zoom out" }));
    expect(transform().scale).toBeCloseTo(fitted);
    fireEvent.click(screen.getByRole("button", { name: "Zoom in" }));
    fireEvent.click(screen.getByRole("button", { name: "Zoom out" }));
    expect(transform().scale).toBeCloseTo(fitted);
    expectFit();
  });
});

import { afterEach, describe, expect, it, vi } from "vitest";
import type { DrawioGraph, DrawioViewerInstance } from "./drawioViewer";
import { fitDrawioViewport } from "./drawioViewport";

function fixture(deferred = false) {
  const host = document.createElement("div");
  host.innerHTML = '<svg style="min-width:1406px;min-height:220px"/>';
  const graph: DrawioGraph = {
    resizeContainer: true,
    getGraphBounds: vi.fn(() => ({x: -10, y: 8, width: 1400, height: 200})),
    addListener: vi.fn(), removeListener: vi.fn(), destroy: vi.fn(),
  };
  const viewer: DrawioViewerInstance = {
    graph: deferred ? null : graph, addListener: vi.fn(), removeListener: vi.fn(),
  };
  return {host, graph, viewer};
}

describe("fitDrawioViewport", () => {
  afterEach(() => { delete window.ResizeSensor; });

  it("fits complete bounds with a guard, preserves aspect and caps natural size", () => {
    const {host, graph, viewer} = fixture();
    fitDrawioViewport(host, viewer);
    const svg = host.querySelector("svg")!;
    expect(svg.getAttribute("viewBox")).toBe("-12 6 1404 204");
    expect(svg.getAttribute("preserveAspectRatio")).toBe("xMinYMin meet");
    expect(svg.style.width).toBe("100%");
    expect(svg.style.height).toBe("auto");
    expect(svg.style.maxWidth).toBe("1404px");
    expect(svg.style.minWidth).toBe("0px");
    expect(host.style.width).toBe("100%");
    expect(graph.resizeContainer).toBe(false);
  });

  it("updates after deferred initialization and later renderer layout, then removes listeners", () => {
    const {host, graph, viewer} = fixture(true);
    const detach = vi.fn();
    window.ResizeSensor = {detach};
    const dispose = fitDrawioViewport(host, viewer);
    expect(graph.addListener).not.toHaveBeenCalled();
    const initialized = vi.mocked(viewer.addListener).mock.calls[0][1];
    viewer.graph = graph;
    initialized();
    initialized();
    expect(graph.addListener).toHaveBeenCalledTimes(1);
    const fit = vi.mocked(graph.addListener).mock.calls[0][1];
    vi.mocked(graph.getGraphBounds).mockReturnValue({x: 0, y: 0, width: 160, height: 60});
    fit();
    expect(host.querySelector("svg")!.style.maxWidth).toBe("164px");
    dispose();
    expect(viewer.removeListener).toHaveBeenCalledWith(initialized);
    expect(graph.removeListener).toHaveBeenCalledWith(fit);
    expect(detach).toHaveBeenCalledWith(host);
    expect(graph.destroy).toHaveBeenCalledOnce();
  });

  it("keeps empty bounds finite and rejects invalid renderer measurements", () => {
    const {host, graph, viewer} = fixture();
    vi.mocked(graph.getGraphBounds).mockReturnValue({x: 0, y: 0, width: 0, height: 0});
    fitDrawioViewport(host, viewer);
    expect(host.querySelector("svg")!.getAttribute("viewBox")).toBe("-2 -2 5 5");
    vi.mocked(graph.getGraphBounds).mockReturnValue({x: NaN, y: 0, width: 0, height: 0});
    vi.mocked(graph.addListener).mock.calls[0][1]();
    expect(host.querySelector("svg")!.getAttribute("viewBox")).toBe("-2 -2 5 5");
  });
});

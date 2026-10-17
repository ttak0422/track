import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { DrawioDiagram } from "./DrawioDiagram";
import type { DrawioViewerInstance } from "./drawioViewer";

const instances: DrawioViewerInstance[] = [];
const createViewerForElement = vi.fn((element: Element, ready?: (viewer: DrawioViewerInstance) => void) => {
  element.innerHTML = '<svg viewBox="0 0 100 40"><text>Boxes</text></svg>';
  const viewer = {
    graph: {resizeContainer: true, getGraphBounds: () => ({x: 0, y: 0, width: 100, height: 40}),
      addListener: vi.fn(), removeListener: vi.fn(), destroy: vi.fn()},
    addListener: vi.fn(), removeListener: vi.fn(),
  };
  instances.push(viewer);
  ready?.(viewer);
});

vi.mock("./drawioViewer", () => ({
  loadDrawioViewer: () => Promise.resolve({ createViewerForElement }),
}));

const mxfile =
  "<mxfile><diagram name=\"Page-1\"><mxGraphModel><root>" +
  '<mxCell id="0" /><mxCell id="1" parent="0" />' +
  '<mxCell id="2" value="A" vertex="1" parent="1"><mxGeometry x="0" y="0" width="80" height="40" as="geometry" /></mxCell>' +
  "</root></mxGraphModel></diagram></mxfile>";

describe("DrawioDiagram", () => {
  afterEach(() => {
    vi.clearAllMocks();
    instances.length = 0;
  });

  it("hands the source to the viewer and renders into the host", async () => {
    const { container } = render(<DrawioDiagram text={mxfile} />);
    await waitFor(() => expect(container.querySelector("svg")).toBeInTheDocument());
    expect(screen.getByRole("img", { name: "draw.io diagram" })).toBeInTheDocument();
    const host = createViewerForElement.mock.calls[0][0] as HTMLElement;
    const config = JSON.parse(host.dataset.mxgraph ?? "{}");
    // The full mxfile rides through untouched (the viewer decodes compressed pages itself);
    // no toolbar, no auto-scan class.
    expect(config.xml).toBe(mxfile);
    expect(config.toolbar).toBeNull();
    expect(config.resize).toBe(false);
    expect(config["auto-fit"]).toBe(false);
    expect(host.classList.contains("mxgraph")).toBe(false);
  });

  it("releases the old viewer when the source changes and when unmounted", async () => {
    const {container, rerender, unmount} = render(<DrawioDiagram text={mxfile} />);
    await waitFor(() => expect(container.querySelector("svg")).toBeInTheDocument());
    const first = instances[0];
    rerender(<DrawioDiagram text={mxfile.replace('value="A"', 'value="B"')} />);
    await waitFor(() => expect(createViewerForElement).toHaveBeenCalledTimes(2));
    expect(first.graph!.destroy).toHaveBeenCalledOnce();
    expect(first.removeListener).toHaveBeenCalledOnce();
    const second = instances[1];
    unmount();
    expect(second.graph!.destroy).toHaveBeenCalledOnce();
  });

  it("accepts a bare mxGraphModel document", async () => {
    const { container } = render(
      <DrawioDiagram text={"<mxGraphModel><root><mxCell id='0'/></root></mxGraphModel>"} />,
    );
    await waitFor(() => expect(container.querySelector("svg")).toBeInTheDocument());
  });

  it("falls back to the message plus source for a non-drawio document", async () => {
    render(<DrawioDiagram text={"just some text"} />);
    await waitFor(() =>
      expect(screen.getByText(/draw\.io render failed/)).toBeInTheDocument(),
    );
    expect(createViewerForElement).not.toHaveBeenCalled();
    // The source stays visible so the note is never a dead end.
    expect(screen.getByText(/just some text/)).toBeInTheDocument();
  });
});

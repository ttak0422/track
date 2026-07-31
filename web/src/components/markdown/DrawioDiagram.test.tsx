import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { DrawioDiagram } from "./DrawioDiagram";

const createViewerForElement = vi.fn((element: Element) => {
  element.innerHTML = '<svg viewBox="0 0 100 40"><text>Boxes</text></svg>';
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
    expect(host.classList.contains("mxgraph")).toBe(false);
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

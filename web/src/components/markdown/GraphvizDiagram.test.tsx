import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { GraphvizDiagram, withTransparentBackground } from "./GraphvizDiagram";

// jsdom does not implement pointer capture (the shared diagram frame's drag relies on it).
beforeAll(() => {
  Element.prototype.setPointerCapture = () => {};
  Element.prototype.releasePointerCapture = () => {};
});

const dot = vi.fn(
  (src: string) =>
    `<?xml version="1.0"?>\n<!DOCTYPE svg>\n<svg viewBox="0 0 62 116"><text>${src.includes("bad") ? "" : "Graph"}</text></svg>`,
);

vi.mock("@hpcc-js/wasm-graphviz", () => ({
  Graphviz: { load: async () => ({ dot }) },
}));

describe("GraphvizDiagram", () => {
  afterEach(() => {
    vi.clearAllMocks();
  });

  it("renders the generated SVG inside the shared diagram frame", async () => {
    const { container } = render(<GraphvizDiagram text={"digraph { a -> b }"} />);
    expect(screen.getByText("Rendering diagram...")).toBeInTheDocument();
    await waitFor(() => expect(container.querySelector("svg")).toBeInTheDocument());
    expect(screen.getByRole("img", { name: "Graphviz diagram" })).toBeInTheDocument();
    expect(container.querySelector(".graphviz-diagram")).toBeInTheDocument();
  });

  it("strips the XML prolog so only the <svg> element is injected", async () => {
    const { container } = render(<GraphvizDiagram text={"digraph { a -> b }"} />);
    await waitFor(() => expect(container.querySelector("svg")).toBeInTheDocument());
    expect(container.innerHTML).not.toContain("DOCTYPE");
  });

  it("passes the source through the transparent-background default", async () => {
    render(<GraphvizDiagram text={"digraph { a -> b }"} />);
    await waitFor(() => expect(dot).toHaveBeenCalled());
    expect(dot.mock.calls[0][0]).toContain('bgcolor="transparent"');
    expect(dot.mock.calls[0][0]).toContain("a -> b");
  });

  it("falls back to the message and source on a render error", async () => {
    dot.mockImplementationOnce(() => {
      throw new Error("syntax error in line 1");
    });
    const { container } = render(<GraphvizDiagram text={"digraph {"} />);
    await waitFor(() =>
      expect(screen.getByText("Graphviz render failed: syntax error in line 1")).toBeInTheDocument(),
    );
    expect(container.querySelector(".code-block")).toBeInTheDocument();
  });
});

describe("withTransparentBackground", () => {
  it("injects bgcolor right after the opening brace", () => {
    expect(withTransparentBackground("digraph G { a -> b }")).toBe(
      'digraph G { bgcolor="transparent";  a -> b }',
    );
  });

  it("leaves brace-less input untouched", () => {
    expect(withTransparentBackground("not dot at all")).toBe("not dot at all");
  });
});

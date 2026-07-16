import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { D2Diagram } from "./D2Diagram";

// jsdom does not implement pointer capture (the shared diagram frame's drag relies on it).
beforeAll(() => {
  Element.prototype.setPointerCapture = () => {};
  Element.prototype.releasePointerCapture = () => {};
});

const compile = vi.fn(
  async (input: { fs: Record<string, string>; options: { themeID: number } }) => {
    if (input.fs.index.includes("bad")) throw new Error("d2 syntax error");
    return { diagram: { name: "" }, renderOptions: { themeID: input.options.themeID, pad: 16 } };
  },
);
const renderSvg = vi.fn(
  async (_diagram: unknown, _options: Record<string, unknown>) =>
    '<svg viewBox="0 0 128 66"><text>Diagram</text></svg>',
);

vi.mock("@terrastruct/d2", () => ({
  D2: class {
    compile = compile;
    render = renderSvg;
  },
}));

describe("D2Diagram", () => {
  afterEach(() => {
    vi.clearAllMocks();
  });

  it("renders the generated SVG inside the shared diagram frame", async () => {
    const { container } = render(<D2Diagram text={"a -> b"} />);
    expect(screen.getByText("Rendering diagram...")).toBeInTheDocument();
    await waitFor(() => expect(container.querySelector("svg")).toBeInTheDocument());
    expect(screen.getByRole("img", { name: "D2 diagram" })).toBeInTheDocument();
  });

  it("compiles the source with a theme id and renders with a unique salt", async () => {
    const { container } = render(<D2Diagram text={"a -> b"} />);
    await waitFor(() => expect(container.querySelector("svg")).toBeInTheDocument());
    expect(compile).toHaveBeenCalledWith({ fs: { index: "a -> b" }, options: { themeID: 0, pad: 16 } });
    const options = renderSvg.mock.calls[0][1];
    expect(options.noXMLTag).toBe(true);
    expect(options.salt).toEqual(expect.any(String));
  });

  it("falls back to the message and source on a compile error", async () => {
    const { container } = render(<D2Diagram text={"bad {"} />);
    await waitFor(() =>
      expect(screen.getByText("D2 render failed: d2 syntax error")).toBeInTheDocument(),
    );
    expect(container.querySelector(".code-block")).toBeInTheDocument();
  });
});

import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { D2Diagram } from "./D2Diagram";

// jsdom does not implement pointer capture (the shared diagram frame's drag relies on it).
beforeAll(() => {
  Element.prototype.setPointerCapture = () => {};
  Element.prototype.releasePointerCapture = () => {};
});

const defaultCompile = async (input: { fs: Record<string, string>; options: { themeID: number } }) => {
  if (input.fs.index.includes("bad")) throw new Error("d2 syntax error");
  return { diagram: { name: "" }, renderOptions: { themeID: input.options.themeID, pad: 16 } };
};
const defaultRender = async (_diagram: unknown, _options: Record<string, unknown>) =>
  '<svg viewBox="0 0 128 66"><text>Diagram</text></svg>';
const compile = vi.fn(defaultCompile);
const renderSvg = vi.fn(defaultRender);

vi.mock("@d2lang/d2", () => ({
  D2: class {
    compile = compile;
    render = renderSvg;
  },
}));

describe("D2Diagram", () => {
  afterEach(() => {
    vi.clearAllMocks();
    compile.mockImplementation(defaultCompile);
    renderSvg.mockImplementation(defaultRender);
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

  it("serializes compile-render pairs on the shared D2 worker", async () => {
    let compileCount = 0;
    let workerResolve: ((value: unknown) => void) | undefined;
    const workerResponse = <T,>(value: T, delay: number) =>
      new Promise<T>((resolve) => {
        // The D2 worker API has one currentResolve slot, so overlapping requests can deliver one
        // request's response to another request's promise.
        workerResolve = resolve as (value: unknown) => void;
        setTimeout(() => workerResolve?.(value), delay);
      });

    compile.mockImplementation((input) => {
      const request = ++compileCount;
      return workerResponse(
        {
          diagram: { name: `diagram-${request}` },
          renderOptions: { themeID: input.options.themeID, pad: 16 },
        },
        request === 1 ? 0 : 10,
      );
    });
    renderSvg.mockImplementation((_diagram, _options) =>
      workerResponse('<svg viewBox="0 0 128 66"><text>Diagram</text></svg>', 20),
    );

    const { container } = render(
      <>
        <D2Diagram text="first" />
        <D2Diagram text="second" />
      </>,
    );

    await waitFor(() => expect(container.querySelectorAll(".mermaid-pan svg")).toHaveLength(2));
    expect(container.querySelectorAll(".mermaid-diagram-loading")).toHaveLength(0);
    expect(container).not.toHaveTextContent("[object Object]");
  });

  it("falls back to the message and source on a compile error", async () => {
    const { container } = render(<D2Diagram text={"bad {"} />);
    await waitFor(() =>
      expect(screen.getByText("D2 render failed: d2 syntax error")).toBeInTheDocument(),
    );
    expect(container.querySelector(".code-block")).toBeInTheDocument();
  });
});

// This Node builtin is test-only; the frontend intentionally excludes @types/node.
// @ts-expect-error node builtin
import { createRequire } from "node:module";
import mermaid from "mermaid";
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

const require = createRequire(import.meta.url);
const originalGetBBox = Object.getOwnPropertyDescriptor(SVGElement.prototype, "getBBox");
let renderSequence = 0;

// JSDOM supplies neither native MathML support nor SVG layout. These shims allow the real public
// Mermaid renderer to run; assertions below cover markup/configuration, not visual geometry.
beforeAll(() => {
  Object.defineProperty(SVGElement.prototype, "getBBox", {
    configurable: true,
    value: () => ({ x: 0, y: 0, width: 100, height: 24 }),
  });
});

afterAll(() => {
  if (originalGetBBox) Object.defineProperty(SVGElement.prototype, "getBBox", originalGetBBox);
  else Reflect.deleteProperty(SVGElement.prototype, "getBBox");
});

beforeEach(() => {
  vi.stubGlobal("MathMLElement", class MathMLElement {});
  vi.stubGlobal("fetch", vi.fn(() => Promise.reject(new Error("Math must not fetch external resources"))));
  vi.spyOn(Element.prototype, "getBoundingClientRect").mockReturnValue({
    x: 0, y: 0, width: 100, height: 24, top: 0, right: 100, bottom: 24, left: 0,
    toJSON: () => ({}),
  });
  mermaid.initialize({ startOnLoad: false, securityLevel: "strict", theme: "base" });
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  document.body.replaceChildren();
});

async function renderMath(expression: string, forceLegacyMathML = false): Promise<HTMLDivElement> {
  const frontmatter = forceLegacyMathML ? "---\nconfig:\n  forceLegacyMathML: true\n---\n" : "";
  const { svg } = await mermaid.render(
    `track-math-dependency-${++renderSequence}`,
    `${frontmatter}flowchart LR\nA["$$${expression}$$"]`,
  );
  const result = document.createElement("div");
  result.innerHTML = svg;
  return result;
}

describe("Mermaid math renderer compatibility", () => {
  it("resolves the reviewed Mermaid renderer to the same KaTeX package as the shared stylesheet", () => {
    const fromMermaid = createRequire(require.resolve("mermaid"));
    expect(fromMermaid.resolve("katex")).toBe(require.resolve("katex"));
    expect(fromMermaid.resolve("katex/dist/katex.min.css")).toBe(require.resolve("katex/dist/katex.min.css"));
  });

  it("keeps native MathML as the default on supporting browsers", async () => {
    const result = await renderMath(String.raw`\sqrt{\frac{x}{2}}`);
    expect(result.querySelector(".katex math")).not.toBeNull();
    expect(result.querySelector(".katex-html")).toBeNull();
    expect(mermaid.mermaidAPI.getConfig().forceLegacyMathML).toBe(false);
    expect(mermaid.mermaidAPI.getConfig().securityLevel).toBe("strict");
  });

  it("matches the shared stylesheet when diagram frontmatter forces legacy HTML math", async () => {
    const result = await renderMath(String.raw`\sqrt{\frac{x}{2}}`, true);
    expect(mermaid.mermaidAPI.getConfig().forceLegacyMathML).toBe(true);
    expect(mermaid.mermaidAPI.getConfig().securityLevel).toBe("strict");
    expect(result.querySelector(".katex-html .katex-base")).not.toBeNull();
    expect(result.querySelector(".katex-html .katex-strut")).not.toBeNull();
    expect(result.querySelector(".katex .base, .katex .strut")).toBeNull();
    expect(result.querySelector(".katex math")).not.toBeNull();
  });

  it.each([false, true])("does not trust image/link/HTML commands (legacy mode: %s)", async (legacy) => {
    const result = await renderMath(String.raw`x+\includegraphics{https://tracker.test/pixel.png}+\href{https://tracker.test/page}{click}+\htmlClass{untrusted-math}{x}`, legacy);
    expect(result.querySelector(".katex")).not.toBeNull();
    expect(result.querySelector("img, image, a, iframe, .untrusted-math")).toBeNull();
    expect(mermaid.mermaidAPI.getConfig().securityLevel).toBe("strict");
    expect(fetch).not.toHaveBeenCalled();
  });

  it("bounds recursive macros and can render a later valid diagram", async () => {
    await expect(renderMath(String.raw`\def\loop{\loop}\loop`, true)).rejects.toThrow("Too many expansions");
    const result = await renderMath("x^2", true);
    expect(result.querySelector(".katex-html .katex-base")).not.toBeNull();
    expect(fetch).not.toHaveBeenCalled();
  });
});

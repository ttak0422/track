// @vitest-environment node
import { renderToString } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";
import { MermaidDiagram } from "./MermaidDiagram";

vi.mock("mermaid", () => { throw new Error("SSR must not load Mermaid"); });
vi.mock("./peleEngine", () => { throw new Error("SSR must not load Pele"); });

describe("diagram static prerender", () => {
  it.each([
    "flowchart LR\nA-->B",
    'flowchart LR\nA["$$x^2$$"]',
  ])("prerenders a loading frame without DOM access or renderer imports: %s", (text) => {
    expect(typeof document).toBe("undefined");
    const html = renderToString(<MermaidDiagram text={text} />);
    expect(html).toContain("Rendering diagram...");
    expect(html).not.toContain("<svg");
  });
});

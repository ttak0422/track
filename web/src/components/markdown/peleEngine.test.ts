import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderPele } from "./peleEngine";
import { popupPeleSvg, sanitizePeleSvg } from "./peleSvg";
import { computeFit } from "./MermaidDiagram";

beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn(() => { throw new Error("No external requests permitted"); }));
  // JSDOM has no canvas text layout; integration tests check markup, not visual geometry.
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue({ measureText: (text: string) => ({ width: text.length * 8 }) } as unknown as CanvasRenderingContext2D);
});
afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); });

describe("local Pele engine", () => {
  it.each([
    'flowchart LR\nA["`**Bold** 日本語`"] --> B[Done]',
    'sequenceDiagram\nAlice->>Bob: Hello\nactivate Bob\nBob-->>Alice: Done\ndeactivate Bob',
    'classDiagram\nAnimal <|-- Duck',
    'stateDiagram-v2\n[*] --> Ready',
    'erDiagram\nUSER ||--o{ NOTE : owns',
    'gantt\ntitle Plan\ndateFormat YYYY-MM-DD\nsection Work\nTask :2026-10-01, 2d',
    'pie\n"One" : 2\n"Two" : 3',
    'architecture-beta\ngroup platform(cloud)[Platform]\nservice api(server)[API] in platform',
  ])("renders representative source locally: %s", async (source) => {
    const { svg } = await renderPele(source, "sans-serif", "monospace");
    expect(svg).toContain('class="pele');
    expect(svg).toContain("viewBox=");
    expect(svg).not.toMatch(/<(?:script|foreignObject|image|a)\b/);
    expect(fetch).not.toHaveBeenCalled();
  });

  it("preserves CSS classes, markdown labels and accessible names", async () => {
    const { svg } = await renderPele('flowchart LR\naccTitle: Test title\naccDescr: Test description\nA["`**太字** 日本語`"] --> B[Done]', "sans-serif", "monospace");
    expect(svg).toContain("Test title");
    expect(svg).toContain("Test description");
    expect(svg).toContain("太字");
    expect(svg).toContain('font-weight="var(--_w)"');
    expect(svg).toContain("var(--pele-fg");
    expect(svg).toContain('data-id="A"');
  });

  it("uses disjoint IDs across diagrams and their popups, preserving references", async () => {
    const source = 'flowchart LR\naccTitle: title\naccDescr: description\nA-->B';
    const first = await renderPele(source, "sans-serif", "monospace");
    const second = await renderPele(source, "sans-serif", "monospace");
    const host = document.createElement("div");
    host.innerHTML = first.svg + first.popupSvg + second.svg + second.popupSvg;
    const ids = [...host.querySelectorAll("[id]")].map((node) => node.id);
    expect(ids.length).toBeGreaterThan(0);
    expect(new Set(ids).size).toBe(ids.length);
    for (const node of host.querySelectorAll("[aria-labelledby], [aria-describedby]")) {
      for (const attribute of ["aria-labelledby", "aria-describedby"]) {
        for (const id of node.getAttribute(attribute)?.split(/\s+/) ?? []) expect(ids).toContain(id);
      }
    }
    const popup = popupPeleSvg('<svg xmlns="http://www.w3.org/2000/svg"><defs><linearGradient id="paint" /></defs><path fill="url(#paint)"/><text>paint</text></svg>');
    expect(popup).toContain('id="paint-popup"');
    expect(popup).toContain('fill="url(#paint-popup)"');
    expect(popup).toContain(">paint</text>");
  });

  it("retains source class styling and edge styling after sanitizing", async () => {
    const { svg } = await renderPele('flowchart LR\nA[Warning]:::warn-->B[Done]\nclassDef warn fill:#c13a1e,stroke:#8e8c84,color:#ffffff\nlinkStyle 0 stroke:#c13a1e,stroke-dasharray:4 4', "sans-serif", "monospace");
    expect(svg).toContain("#c13a1e");
    expect(svg).toContain("#8e8c84");
    expect(svg).toContain("stroke-dasharray");
  });

  it("fits a wide Pele SVG to a narrow reading frame without changing source direction", async () => {
    const { svg } = await renderPele('flowchart LR\nA[Start]-->B[Middle]-->C[Done]', "sans-serif", "monospace");
    const root = new DOMParser().parseFromString(svg, "image/svg+xml").documentElement;
    const [, , width, height] = root.getAttribute("viewBox")!.split(/\s+/).map(Number);
    expect(width).toBeGreaterThan(height);
    const fit = computeFit(width, height, 240);
    expect(width * fit.transform.scale).toBeLessThanOrEqual(240);
  });

  it.each([
    'flowchart LR\nA-->B\nclick A "https://tracker.invalid"',
    'flowchart LR\nA@{ img: "https://tracker.invalid/pixel.png", label: "Image", w: 80, h: 50 }',
  ])("disables diagram links/images and rejects oversized source: %s", async (source) => {
    const { svg } = await renderPele(source, "sans-serif", "monospace");
    expect(svg).not.toContain("tracker.invalid");
    expect(svg).not.toMatch(/<(?:a|image)\b/);
    await expect(renderPele('flowchart LR\n' + ' '.repeat(50_001), "sans-serif", "monospace")).rejects.toThrow();
    expect(fetch).not.toHaveBeenCalled();
  });

  it("sanitizes active content and external CSS while retaining local gradients and variables", () => {
    const svg = sanitizePeleSvg('<svg xmlns="http://www.w3.org/2000/svg" class="pele" style="--_fg:var(--pele-fg);color:var(--_fg)"><style>body{color:red}</style><script>alert(1)</script><image href="https://tracker.invalid"/><a href="javascript:alert(1)"><text>label</text></a><path onload="alert(1)" fill="url(https://tracker.invalid)"/><path stroke="url(#paint)"/><rect style="fill:u\\72l(https://tracker.invalid)"/></svg>');
    expect(svg).not.toMatch(/tracker|onload|<script|<style|<image|<a\b/);
    expect(svg).toContain("var(--pele-fg)");
    expect(svg).toContain("url(#paint)");
    expect(svg).toContain('class="pele"');
  });
});

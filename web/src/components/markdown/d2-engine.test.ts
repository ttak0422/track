// @vitest-environment node
import { D2 } from "@d2lang/d2";
import { afterAll, describe, expect, it } from "vitest";

const engine = new D2();
afterAll(() => engine.dispose());

describe("bundled D2 engine", () => {
  it.each(["dagre", "elk", "tala"])("compiles and renders %s selected by the note", async (layout) => {
    const result = await engine.compile({
      fs: { index: `vars: { d2-config: { layout-engine: ${layout} } }\napi -> database` },
      options: { pad: 16 },
    });
    const svg = await engine.render(result.diagram, { ...result.renderOptions, noXMLTag: true });
    expect(svg).toContain('data-d2-version="v0.9.0"');
    expect(svg).toContain("<svg");
    expect(svg).toContain("database");
    expect(result.diagram.shapes).toHaveLength(2);
  }, 20000);

  it("reports an unsupported layout and renders a later valid TALA diagram", async () => {
    await expect(engine.compile('vars: { d2-config: { layout-engine: missing } }\na -> b')).rejects.toThrow();
    const result = await engine.compile("a -> b", { layout: "tala" });
    expect(await engine.render(result.diagram)).toContain("<svg");
  }, 20000);
});

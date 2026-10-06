// These Node builtins are test-only; the frontend intentionally excludes @types/node.
// @ts-expect-error node builtin
import { readFileSync } from "node:fs";
// @ts-expect-error node builtin
import { createRequire } from "node:module";
import { describe, expect, it } from "vitest";

const require = createRequire(import.meta.url);

describe("KaTeX renderer and stylesheet compatibility", () => {
  it("resolves the reviewed HTML renderers to the same KaTeX package as the stylesheet", () => {
    for (const adapter of ["rehype-katex", "mermaid"]) {
      const fromAdapter = createRequire(require.resolve(adapter));
      expect(fromAdapter.resolve("katex")).toBe(require.resolve("katex"));
      expect(fromAdapter.resolve("katex/dist/katex.min.css")).toBe(require.resolve("katex/dist/katex.min.css"));
    }
  });

  it("ships the prefixed layout rules required by the rendered math regression fixtures", () => {
    // Vitest's default CSS stub is empty, even for a ?raw import. Read the actual installed CSS.
    const stylesheet = readFileSync(require.resolve("katex/dist/katex.min.css"), "utf8");
    for (const name of ["katex-base", "katex-strut", "katex-root", "katex-sizing", "katex-overline", "katex-underline"]) {
      expect(stylesheet).toContain(`.${name}`);
    }
  });
});

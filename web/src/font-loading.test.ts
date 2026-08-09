// @ts-expect-error node builtin — no @types/node installed on purpose (tests run in Node)
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

const html = readFileSync("index.html", "utf8");
const css = readFileSync("src/styles.css", "utf8");
const fontStylesheet =
  "https://fonts.googleapis.com/css2?family=IBM+Plex+Sans+JP:wght@400;500;600;700&display=swap";

describe("IBM Plex Sans JP loading", () => {
  it("keeps the font stylesheet out of the render-blocking CSS bundle", () => {
    expect(css).not.toContain("fonts.googleapis.com");
    expect(css).not.toMatch(/@import\b/);
  });

  it("loads the stylesheet asynchronously without blocking the first paint", () => {
    const document = new DOMParser().parseFromString(html, "text/html");
    const stylesheet = Array.from(
      document.querySelectorAll<HTMLLinkElement>('link[rel="stylesheet"]'),
    ).find((link) => link.href === fontStylesheet && link.media === "print");

    expect(stylesheet).toBeDefined();
    expect(stylesheet?.getAttribute("onload")).toBe("this.media='all'");
    expect(
      document.querySelector('link[rel="preconnect"][href="https://fonts.googleapis.com"]'),
    ).not.toBeNull();
    expect(
      document.querySelector('link[rel="preconnect"][href="https://fonts.gstatic.com"][crossorigin]'),
    ).not.toBeNull();
  });

  it("retains a synchronous fallback when JavaScript is disabled", () => {
    const fallback = html.match(/<noscript>([\s\S]*?)<\/noscript>/)?.[1] ?? "";

    expect(fallback).toContain(`rel="stylesheet"`);
    expect(fallback).toContain(`href="${fontStylesheet}"`);
  });
});

// @ts-expect-error node builtin — no @types/node installed on purpose (tests run in Node)
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

const html = readFileSync("index.html", "utf8");
const css = readFileSync("src/styles.css", "utf8");
const main = readFileSync("src/main.tsx", "utf8");
const fontStylesheet =
  "https://fonts.googleapis.com/css2?family=IBM+Plex+Sans+JP:wght@400;500;600;700&display=swap";

describe("IBM Plex Sans JP loading", () => {
  it("keeps the font stylesheet out of the render-blocking CSS bundle", () => {
    expect(css).not.toContain("fonts.googleapis.com");
    expect(css).not.toMatch(/@import\b/);
  });

  it("injects the stylesheet only after the first paint, from the entry loader", () => {
    const document = new DOMParser().parseFromString(html, "text/html");
    // No <link> for the fonts may sit in the head: a request that starts before first paint lands in
    // render-time metrics' dependency graph without changing a pixel of that paint (main.tsx explains).
    // The noscript fallback is allowed — it only loads when there is no paint at all.
    const links = Array.from(document.querySelectorAll<HTMLLinkElement>('link[rel="stylesheet"]')).filter(
      (link) => link.closest("noscript") === null,
    );
    expect(links).toHaveLength(0);
    expect(document.querySelector('link[rel="preconnect"][href="https://fonts.googleapis.com"]')).not.toBeNull();
    expect(
      document.querySelector('link[rel="preconnect"][href="https://fonts.gstatic.com"][crossorigin]'),
    ).not.toBeNull();
    // The loader owns the injection and waits out a frame first.
    expect(main).toContain(fontStylesheet);
    expect(main).toContain("afterFirstPaint");
  });

  it("retains a synchronous fallback when JavaScript is disabled", () => {
    const fallback = html.match(/<noscript>([\s\S]*?)<\/noscript>/)?.[1] ?? "";

    expect(fallback).toContain(`rel="stylesheet"`);
    expect(fallback).toContain(`href="${fontStylesheet}"`);
  });
});

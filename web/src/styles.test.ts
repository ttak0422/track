// @ts-expect-error node builtin — no @types/node installed on purpose (tests run in Node)
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

const css = readFileSync("src/styles.css", "utf8");
const shell = readFileSync("src/components/Shell.tsx", "utf8");
const mermaid = readFileSync("src/components/markdown/MermaidDiagram.tsx", "utf8");

function ruleBody(selector: string): string {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return css.match(new RegExp(`${escaped}\\s*\\{([^}]*)\\}`))?.[1] ?? "";
}

function zIndex(selector: string): number | undefined {
  const value = ruleBody(selector).match(/z-index:\s*(\d+)/)?.[1];
  return value === undefined ? undefined : Number(value);
}

describe("special-page layering", () => {
  it("keeps tabbar popups above the full-bleed graph view", () => {
    const tabstripZ = zIndex(".tabstrip");
    const graphZ = zIndex(".graph-full");

    expect(tabstripZ).toBeDefined();
    expect(graphZ).toBeDefined();
    expect(tabstripZ).toBeGreaterThan(graphZ!);
  });

  it("keeps ordinary special-page content below the tab strip", () => {
    const tabstripZ = zIndex(".tabstrip")!;

    for (const selector of [".calendar-full", ".day-view", ".tasks-view", ".home-hero"]) {
      expect(zIndex(selector) ?? Number.NEGATIVE_INFINITY).toBeLessThan(tabstripZ);
    }
  });
});

describe("content width", () => {
  it("lets the Content width setting reach prose blocks", () => {
    const proseRule = css.match(/\.markdown-view > \*\s*\{([^}]*)\}/)?.[1] ?? "";

    expect(proseRule).toMatch(/max-width:\s*var\(--content-measure\)/);
  });

  it("lets the Content width setting reach note metadata", () => {
    const propsRule = css.match(/\.note-props\s*\{([^}]*)\}/)?.[1] ?? "";

    expect(propsRule).toMatch(/max-width:\s*var\(--content-measure\)/);
  });

  it("lets shared diagram viewports bleed to the window while keeping frame chrome in the prose column", () => {
    const viewportRule = ruleBody(".markdown-view > .mermaid-diagram > .mermaid-viewport");

    expect(viewportRule).toMatch(/width:\s*100vw/);
    expect(viewportRule).toMatch(/max-width:\s*100vw/);
    expect(viewportRule).toMatch(/margin-left:\s*calc\(50%\s*-\s*50vw\)/);
    expect(viewportRule).not.toMatch(/position:\s*relative/);
    expect(css).not.toMatch(/\.markdown-view > \.mermaid-diagram\s*\{[^}]*width:\s*100vw/);
  });

  it("clips full-bleed diagrams without taking ownership of vertical reading scroll", () => {
    const readerRule = ruleBody(".reader");

    expect(readerRule).toMatch(/overflow-x:\s*clip/);
    expect(readerRule).toMatch(/overflow-y:\s*auto/);
  });

  it("leaves horizontal overflow to tables and diagram viewports", () => {
    expect(ruleBody(".markdown-view table")).toMatch(/overflow-x:\s*auto/);
    expect(ruleBody(".mermaid-viewport")).toMatch(/position:\s*relative/);
    expect(ruleBody(".mermaid-viewport")).toMatch(/overflow:\s*hidden/);
  });
});

describe("diagram controls", () => {
  it("keeps the expand control in the shared quiet-chip row", () => {
    const controlsRule = css.match(/(?:^|\n)\.mermaid-controls\s*\{([^}]*)\}/)?.[1] ?? "";

    expect(controlsRule).toMatch(/display:\s*flex/);
    expect(controlsRule).toMatch(/flex-direction:\s*row/);
    expect(mermaid).toMatch(/<div className="mermaid-controls">[\s\S]*className="mermaid-control mermaid-open"/);
  });
});

describe("sidebar at short viewport heights", () => {
  it("reserves the tab row and an 8px gap above a quarter-centred rail", () => {
    const reservedTabRow = Number(ruleBody(".reader-pane").match(/padding-top:\s*(\d+)px/)?.[1]);
    const railHeightDeduction = Number(
      ruleBody(".activity-rail").match(/max-height:\s*calc\(50vh\s*-\s*(\d+)px\)/)?.[1],
    );

    expect(reservedTabRow).toBeGreaterThan(0);
    expect(railHeightDeduction).toBeGreaterThanOrEqual(2 * (reservedTabRow + 8));
  });

  it("keeps Settings outside the scrolling group so it remains reachable", () => {
    expect(ruleBody(".rail-scroll")).toMatch(/overflow-y:\s*auto/);
    expect(shell).toMatch(
      /<nav className="activity-rail"[\s\S]*<div className="rail-scroll">[\s\S]*<\/div>[\s\S]*?<ThemeMenu \/>/,
    );
  });
});

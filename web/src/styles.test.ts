// @ts-expect-error node builtin — no @types/node installed on purpose (tests run in Node)
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

const css = readFileSync("src/styles.css", "utf8");
const previewStack = readFileSync("src/components/preview/stack.ts", "utf8");
const shell = readFileSync("src/components/Shell.tsx", "utf8");

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

describe("transient layer ordering", () => {
  it("keeps aside list rows intact so capped lists can scroll", () => {
    expect(ruleBody(".note-aside .backlink")).toMatch(/flex:\s*0\s+0\s+auto/);
    expect(ruleBody(".backlink-list")).toMatch(/max-height:\s*320px/);
  });

  it("keeps search above previews but below modal and toast layers", () => {
    const previewBaseZ = Number(previewStack.match(/previewBaseZIndex\s*=\s*(\d+)/)?.[1]);
    const previewMaxZ = Number(previewStack.match(/previewMaxZIndex\s*=\s*(\d+)/)?.[1]);
    const searchBackdropZ = zIndex(".search-backdrop")!;
    const searchPopupZ = zIndex(".search-popup")!;

    expect(searchBackdropZ).toBeGreaterThan(previewBaseZ);
    expect(searchBackdropZ).toBeGreaterThan(previewMaxZ);
    expect(searchPopupZ).toBeGreaterThan(searchBackdropZ);
    expect(searchPopupZ).toBeLessThan(zIndex(".modal-backdrop")!);
    expect(searchPopupZ).toBeLessThan(zIndex(".notification-toast")!);
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

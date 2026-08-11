// @ts-expect-error node builtin — no @types/node installed on purpose (tests run in Node)
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

const css = readFileSync("src/styles.css", "utf8");
const previewStack = readFileSync("src/components/preview/stack.ts", "utf8");
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

  it("lets shared diagram viewports bleed to the window while keeping frame chrome in the prose column", () => {
    const viewportRule = ruleBody(".markdown-view > .mermaid-diagram > .mermaid-viewport");

    expect(viewportRule).toMatch(/width:\s*100%/);
    expect(css).toMatch(/\.markdown-view > \.mermaid-diagram > \.mermaid-viewport:not\(\[data-collapsed\]\)\s*\{[^}]*width:\s*100vw/);
    expect(css).toMatch(/\.markdown-view > \.mermaid-diagram > \.mermaid-viewport:not\(\[data-collapsed\]\)\s*\{[^}]*margin-left:\s*calc\(50%\s*-\s*50vw\)/);
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

  it("keeps popup content from drawing a redundant scrollbar", () => {
    const popupRule = ruleBody(".diagram-lightbox-content");
    const popupViewportRule = ruleBody(".diagram-lightbox-content .mermaid-viewport");

    expect(popupRule).toMatch(/overflow:\s*hidden/);
    expect(popupViewportRule).toMatch(/width:\s*100%/);
    expect(popupViewportRule).toMatch(/height:\s*100%/);
  });

  it("uses the bare glyph-button treatment for diagram controls and fold chips", () => {
    const controlRule = css.match(/(?:^|\n)\.mermaid-control\s*\{([^}]*)\}/)?.[1] ?? "";
    const hoverRule = css.match(/\.mermaid-control:hover,[\s\S]*?\.mermaid-control:focus-visible\s*\{([^}]*)\}/)?.[1] ?? "";

    expect(controlRule).toMatch(/border:\s*0/);
    expect(controlRule).toMatch(/background:\s*transparent/);
    expect(controlRule).toMatch(/color:\s*var\(--muted\)/);
    expect(hoverRule).toMatch(/background:\s*var\(--panel-soft\)/);
    expect(hoverRule).toMatch(/border-radius:\s*var\(--radius-sm\)/);
  });

  it("anchors the collapsed fold chip at the frame's top-left", () => {
    const collapsedRule = css.match(
      /\.mermaid-diagram\[data-collapsed\] \.mermaid-fold\s*\{\s*top:[^}]*\}/,
    )?.[0] ?? "";

    expect(collapsedRule).toMatch(/top:\s*8px/);
    expect(collapsedRule).toMatch(/bottom:\s*auto/);
    expect(collapsedRule).toMatch(/left:\s*8px/);
    expect(collapsedRule).toMatch(/transform:\s*none/);
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

describe("enlarged local graph", () => {
  it("uses a viewport-relative size without a desktop pixel cap", () => {
    const lightboxRule = ruleBody(".graph-lightbox");

    expect(lightboxRule).toMatch(/width:\s*80vw/);
    expect(lightboxRule).toMatch(/height:\s*80vh/);
    expect(css).toMatch(
      /@media\s*\(max-width:\s*960px\)[\s\S]*?\.graph-lightbox\s*\{[\s\S]*?width:\s*92vw[\s\S]*?height:\s*84vh/,
    );
  });
});

describe("modal layout stability", () => {
  it("keeps the visible note scrollbar's gutter stable while a popup changes scrolling", () => {
    const noteReaderRule =
      css.match(/\.reader:has\(\.note-reader\):not\(:has\(\.note-editor textarea\)\)\s*\{([^}]*)\}/)?.[1] ?? "";

    expect(noteReaderRule).toMatch(/scrollbar-width:\s*thin/);
    expect(noteReaderRule).toMatch(/scrollbar-gutter:\s*stable/);
    expect(css).toMatch(
      /\.reader:has\(\.note-reader\):not\(:has\(\.note-editor textarea\)\)::\-webkit-scrollbar\s*\{[^}]*display:\s*block/,
    );
  });
});

describe("sidebar at short viewport heights", () => {
  it("hangs the dock from a fixed anchor, so an added entry does not move the rest", () => {
    const sidebar = ruleBody(".sidebar");

    // Centring is what made the top edge a function of the dock's own height.
    expect(sidebar).not.toMatch(/align-items:\s*center/);
    expect(sidebar).toMatch(/align-items:\s*flex-start/);
    expect(ruleBody(":root")).toMatch(/--rail-anchor:\s*max\(/);
  });

  it("keeps the anchor clear of the tab row", () => {
    const reservedTabRow = Number(ruleBody(".reader-pane").match(/padding-top:\s*(\d+)px/)?.[1]);
    const anchorFloor = Number(ruleBody(":root").match(/--rail-anchor:\s*max\((\d+)px/)?.[1]);

    expect(reservedTabRow).toBeGreaterThan(0);
    expect(anchorFloor).toBeGreaterThanOrEqual(reservedTabRow + 8);
  });

  // The guides in the empty state point at the rail's buttons. They line up only while both hang from
  // the same place — they drifted once already, when the dock stopped being centred and the guides
  // were left centred. One anchor, read by both.
  it("hangs the empty state's guides from the rail's own anchor", () => {
    expect(ruleBody(":root")).toMatch(/--rail-anchor:/);
    expect(ruleBody(".sidebar")).toMatch(/top:\s*var\(--rail-anchor\)/);
    expect(ruleBody(".empty-guides")).toMatch(/top:\s*calc\(var\(--rail-anchor\)/);
    // Centring is what broke it before: the guides must not re-acquire a centre of their own.
    expect(ruleBody(".empty-guides")).not.toMatch(/translate:/);
  });

  it("keeps Settings outside the scrolling group so it remains reachable", () => {
    expect(ruleBody(".rail-scroll")).toMatch(/overflow-y:\s*auto/);
    expect(shell).toMatch(
      /<nav className="activity-rail"[\s\S]*<div className="rail-scroll">[\s\S]*<\/div>[\s\S]*?<ThemeMenu \/>/,
    );
  });
});

describe("arrival highlight", () => {
  it("fades out instead of tinting the block for as long as the note stays open", () => {
    // The class outlives the animation (it is removed only when the hash changes), so a resting
    // background here is a highlight that never goes away.
    expect(ruleBody(".block-target")).not.toMatch(/background:/);
    expect(css).toMatch(/@keyframes block-flash\s*\{[\s\S]*?100%\s*\{\s*background:\s*transparent;/);
  });
});

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

// Everything a media query declares, joined: a query the sheet opens more than once — each block
// beside the component it belongs to — reads here as the one set of rules it is.
function mediaBody(query: string): string {
  const escaped = query.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return [...css.matchAll(new RegExp(`@media ${escaped} \\{([\\s\\S]*?)\\n\\}`, "g"))]
    .map((match) => match[1])
    .join("\n");
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

describe("tab strip", () => {
  // A tab sized to its own title puts the close button somewhere different on every one, which is what
  // made closing a run of them a chase.
  it("gives every tab the same frame", () => {
    const tab = ruleBody(".tab");

    expect(tab).toMatch(/flex:\s*0\s+0\s+\d+px/);
    expect(tab).not.toMatch(/max-width:/);
    // The basis alone does not hold it: a flex item's automatic minimum size is its content's, so an
    // unbreakable title (a Japanese one has no spaces) stretched the tab past the basis.
    expect(tab).toMatch(/min-width:\s*0/);
    const title = css.match(/\n\.tab-title\s*\{([^}]*)\}/)?.[1] ?? "";
    expect(title).toMatch(/text-overflow:\s*ellipsis/);
  });

  // A vault name is free-form; the title it annotates is the point of the tab.
  it("keeps the vault name to a corner of the frame", () => {
    const layout = ruleBody(".tab-label .tab-vault");

    expect(layout).toMatch(/max-width:\s*40%/);
    expect(layout).toMatch(/text-overflow:\s*ellipsis/);
  });

  // The button stands on the tail of the title it closes. Without a fill arriving alongside it, the
  // glyph is drawn straight onto the letters and neither can be read.
  it("gives the close glyph an opaque ground as it appears", () => {
    const revealed = css.match(/\.tab:hover \.tab-close,\s*\.tab-close:focus-visible\s*\{([^}]*)\}/)?.[1] ?? "";

    expect(revealed).toMatch(/background:\s*var\(--panel-soft\)/);
    expect(css).toMatch(/\.tab:hover \.tab-close-glyph/);
  });

  // The vault a note came from names the tab, it does not control anything: it joins the shared
  // section label recipe (design.md variant 6) and keeps only its own spacing.
  it("writes the vault name as a section label, not a filled badge", () => {
    const rules = [...css.matchAll(/\.tab-vault[^{]*\{([^}]*)\}/g)].map((m) => m[1]);

    expect(rules).toHaveLength(3);
    expect(rules[0]).toMatch(/text-transform:\s*uppercase/);
    for (const rule of rules.slice(1)) {
      expect(rule).not.toMatch(/background|border-radius|padding/);
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

describe("phone width", () => {
  const phone = mediaBody("(max-width: 540px)");

  it("lays the dock along the foot of the window", () => {
    expect(phone).toMatch(/\.sidebar\s*\{[^}]*top:\s*auto/);
    expect(phone).toMatch(/\.sidebar\s*\{[^}]*bottom:\s*0/);
    expect(phone).toMatch(/\.activity-rail\s*\{[^}]*flex-direction:\s*row/);
    expect(phone).toMatch(/\.rail-scroll\s*\{[^}]*flex-direction:\s*row/);
  });

  // The dock's height is one measurement. Everything pinned to the bottom corner adds it, which is
  // why none of them needs a media query of its own — the token is zero while the dock is vertical.
  it("clears the foot dock from a single measurement", () => {
    expect(ruleBody(":root")).toMatch(/--foot-dock:\s*0px/);
    expect(phone).toMatch(/--foot-dock:\s*calc\(/);
    expect(phone).toMatch(/\.reader\s*\{[^}]*var\(--foot-dock\)/);

    for (const selector of [
      ".graph-panel",
      ".graph-fab",
      ".notification-toast",
      // The full-bleed graph fills the reader, which the dock floats over.
      ".graph-full .graph-controls",
    ]) {
      expect(ruleBody(selector)).toMatch(/bottom:\s*calc\([^)]*var\(--foot-dock\)\)/);
    }
  });

  // A full-width frame settles TabBar's own measuring pass on one tab: the leftmost, which is always
  // the note being read. Everything behind it waits in the +N menu instead of splitting the strip.
  it("gives the strip to the tab being read", () => {
    expect(phone).toMatch(/\.tab\s*\{[^}]*flex:\s*1\s+0\s+100%/);
  });

  // 100vh on a phone is the height with the toolbars retracted, so the foot of the workspace — and
  // the dock docked to it — sat behind the toolbar that was on screen.
  it("measures the workspace against the viewport the browser is showing", () => {
    expect(ruleBody(".workspace")).toMatch(/height:\s*100dvh/);
  });
});

describe("pointers that cannot hover", () => {
  const touch = mediaBody("(hover: none)");

  it("opens none of the surfaces hover opens", () => {
    expect(touch).toMatch(/\.tab-tools\s*\{[^}]*display:\s*none/);
    expect(touch).toMatch(/\.media-preview\s*\{[^}]*display:\s*none/);
    // The note and graph previews are JS, and ask the same question of the same pointer.
    expect(previewStack).toMatch(/matchMedia\?\.\("\(hover: none\)"\)/);
  });

  // A reveal that never fires leaves a control that cannot be found: close is the only way out of a
  // tab, and the media chips are the only way into the lightbox.
  it("settles the states a hover would have revealed", () => {
    expect(touch).toMatch(/\.tab-close-glyph\s*\{[^}]*opacity:\s*1/);
    expect(touch).toMatch(/\.media-controls\s*\{[^}]*opacity:\s*1/);
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

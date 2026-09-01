// @ts-expect-error node builtin — no @types/node installed on purpose (tests run in Node)
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

const css = readFileSync("src/styles.css", "utf8");
const previewStack = readFileSync("src/components/preview/stack.ts", "utf8");
const railAnchor = readFileSync("src/components/railAnchor.ts", "utf8");
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

  // The tab being read is the one whose title matters, so it takes twice the frame; closing it hands
  // the wide frame to the next active tab, keeping the close button's landing spot stable.
  it("gives the active tab twice the frame, except on a phone", () => {
    expect(ruleBody(".tab.active")).toMatch(/flex:\s*0\s+0\s+336px/);
    const phone = mediaBody("(max-width: 540px)");
    expect(phone).toMatch(/\.tab\.active\s*\{[^}]*flex:\s*1\s+0\s+100%/);
  });

  // The strip is read by finding the one title that is not faded, so the gap between the tab being
  // read and the rest is wider than chrome's usual muted/ink pair. It is carried by the type, not by
  // a second dot: the strip already has one, and it means unsaved changes.
  it("stands the active tab out by ink against faint, with no dot of its own", () => {
    expect(ruleBody(".tab")).toMatch(/color:\s*var\(--faint\)/);
    expect(ruleBody(".tab.active")).toMatch(/color:\s*var\(--text\)/);
    expect(css).not.toMatch(/\.tab\.active[^{]*::before/);
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

describe("note state badges", () => {
  it("writes NEW in the salient and stale in faint, as label-typography chips", () => {
    const badge = ruleBody(".note-state-badge");

    expect(badge).toMatch(/font-family:\s*var\(--font-mono\)/);
    expect(badge).toMatch(/border-radius:\s*var\(--radius-sm\)/);
    expect(ruleBody(".note-state-new")).toMatch(/color:\s*var\(--mark\)/);
    expect(ruleBody(".note-state-stale")).toMatch(/color:\s*var\(--faint\)/);
  });
});

describe("note flags", () => {
  it("badges flagged notes in the danger red, as the same label-typography chip as the state badges", () => {
    const badge = ruleBody(".note-flag-badge");

    expect(badge).toMatch(/font-family:\s*var\(--font-mono\)/);
    expect(badge).toMatch(/border-radius:\s*var\(--radius-sm\)/);
    expect(ruleBody(".note-flag-badge-deprecated,\n.note-flag-badge-confidential")).toMatch(
      /color:\s*var\(--danger\)/,
    );
  });

  it("stamps a flagged note in danger, uppercase and rotated, over the article but under the floating layers", () => {
    const stamp = ruleBody(".stamp");

    expect(stamp).toMatch(/position:\s*absolute/);
    expect(stamp).toMatch(/color:\s*var\(--danger\)/);
    expect(stamp).toMatch(/text-transform:\s*uppercase/);
    expect(stamp).toMatch(/transform:\s*rotate\(-8deg\)/);
    expect(stamp).toMatch(/opacity:\s*0\.85/);
    expect(stamp).toMatch(/pointer-events:\s*none/);
  });

  it("anchors the stamps to the note article and keeps their layer below every floating one", () => {
    const previewBaseZ = Number(previewStack.match(/previewBaseZIndex\s*=\s*(\d+)/)?.[1]);
    const stampsZ = zIndex(".note-stamps")!;

    expect(ruleBody(".note-reader")).toMatch(/position:\s*relative/);
    expect(ruleBody(".note-editor")).toMatch(/position:\s*relative/);
    expect(ruleBody(".note-stamps")).toMatch(/pointer-events:\s*none/);
    expect(stampsZ).toBeLessThan(previewBaseZ);
    expect(stampsZ).toBeLessThan(zIndex(".selection-copy")!);
  });
});

describe("notification toast", () => {
  it("gives up its shadow for the countdown bar, which drains in the accent", () => {
    const toast = ruleBody(".notification-toast");

    expect(toast).not.toMatch(/box-shadow/);
    expect(toast).toMatch(/overflow:\s*hidden/);
    // The main rule carries the accent fill; the reduced-motion override hides the bar instead.
    const timer = css.match(/\.notification-timer\s*\{([^}]*background:\s*var\(--mark\)[^}]*)\}/)?.[1] ?? "";
    expect(timer).toMatch(/animation:\s*notification-drain/);
    expect(css).toMatch(/@keyframes notification-drain/);
    expect(ruleBody(".notification-timer")).toMatch(/display:\s*none/);
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

  it("anchors the docked bleed at the reading surface's edge, not the middle of the prose", () => {
    const docked = mediaBody("(min-width: 1100px)");

    // Undo the reader's dock lane and the preview's own padding, then whatever margin auto-centring
    // gave the column — the prose's midpoint is not the surface's once the rail sits beside it.
    expect(docked).toMatch(/margin-left:\s*calc\(\s*-64px - var\(--preview-pad-inline, 16px\)/);
    expect(docked).toMatch(/100vw - 96px/);
    // The padding it undoes has to be the one .note-preview actually sets, or the block lands short.
    expect(ruleBody(".note-preview")).toMatch(/--preview-pad-inline:\s*16px/);
    expect(ruleBody(".note-preview")).toMatch(/padding:\s*16px var\(--preview-pad-inline\)/);
    // Uncapped, the column starts at the lane with no centring margin to undo.
    expect(docked).toMatch(
      /html\[data-content-width="none"\][\s\S]*?margin-left:\s*calc\(-64px - var\(--preview-pad-inline, 16px\)\)/,
    );
  });

  it("gives the docked aside padded ground, so a bleeding block cannot reach its text", () => {
    const docked = mediaBody("(min-width: 1100px)");
    const aside = docked.match(/> \.note-aside \{([^}]*)\}/)?.[1] ?? "";

    expect(aside).toMatch(/background:\s*var\(--panel\)/);
    expect(aside).toMatch(/padding:\s*var\(--aside-pad\)/);
    // Ground flush with a glyph is not ground: it has to reach past the words, by the measure the
    // rest of the page keeps.
    expect(aside).toMatch(/--aside-pad:\s*16px/);
    // It grows outward only — added to the column's width, taken back off its margins — so neither
    // the words nor the row move.
    expect(aside).toMatch(/flex:\s*0 0 calc\(clamp\(240px, 24vw, 380px\) \+ var\(--aside-pad\) \* 2\)/);
    expect(aside).toMatch(/margin-inline:\s*calc\(-1 \* var\(--aside-pad\)\)/);
    expect(aside).toMatch(/margin-top:\s*calc\(-1 \* var\(--aside-pad\)\)/);
    expect(aside).toMatch(/top:\s*calc\(32px - var\(--aside-pad\)\)/);
    // The ground carries text over a diagram; it does not draw a box around the column.
    expect(aside).not.toMatch(/border(?!-)/);
    expect(aside).not.toMatch(/border-radius/);
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
    expect(hoverRule).toMatch(/background:\s*var\(--panel\)/);
    expect(hoverRule).toMatch(/border-radius:\s*var\(--radius-sm\)/);
  });

  it("lets the collapsed fold chip carry its label without clipping it", () => {
    const collapsedRule =
      css.match(/\.mermaid-diagram\[data-collapsed\] \.mermaid-fold\s*\{([^}]*)\}/)?.[1] ?? "";

    // Collapsed, the chip is a caret plus "Show full diagram", so the icon-sized square has to give.
    expect(collapsedRule).toMatch(/grid-auto-flow:\s*column/);
    expect(collapsedRule).toMatch(/min-width:\s*max-content/);
    // It rides the strip like every other control, so it takes no corner of its own.
    expect(collapsedRule).not.toMatch(/position:|top:|left:|transform:/);
  });
});

describe("diagram controls", () => {
  it("keeps the expand control in the shared quiet-chip row", () => {
    const controlsRule = css.match(/(?:^|\n)\.mermaid-controls\s*\{([^}]*)\}/)?.[1] ?? "";

    expect(controlsRule).toMatch(/display:\s*flex/);
    expect(controlsRule).toMatch(/flex-direction:\s*row/);
    expect(mermaid).toMatch(/<div className="mermaid-controls">[\s\S]*className="mermaid-control mermaid-open"/);
  });

  it("keeps every control off the drawing, in one strip above it", () => {
    // Both the fold chip and the control row are children of the strip, and the strip precedes the
    // viewport — nothing is left floating in a corner of the drawing.
    expect(mermaid).toMatch(
      /<div className="mermaid-bar">[\s\S]*className="mermaid-control mermaid-fold"[\s\S]*<div className="mermaid-controls">[\s\S]*<\/div>\s*<div\s+className="mermaid-viewport"/,
    );

    // Anchored: .media-frame's own override of the strip comes earlier in the sheet.
    const barRule = css.match(/(?:^|\n)\.mermaid-bar\s*\{([^}]*)\}/)?.[1] ?? "";
    expect(barRule).toMatch(/display:\s*flex/);
    expect(barRule).toMatch(/justify-content:\s*space-between/);
    expect(barRule).toMatch(/background:\s*var\(--panel-soft\)/);
    expect(barRule).toMatch(/border-radius:\s*var\(--radius\)/);
    // Out of the drawing there is nothing to obscure, so the strip does not wait for a hover — which
    // is also the only way a touch pointer ever reaches these controls.
    expect(css).not.toMatch(/\.mermaid-diagram:hover \.mermaid-(controls|fold)/);
    expect(ruleBody(".mermaid-controls")).not.toMatch(/opacity:\s*0/);
    // The popup keeps its floating cluster: its drawing is fitted inside the window's own padding.
    expect(ruleBody(".diagram-lightbox-controls")).toMatch(/position:\s*absolute/);
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
  // The dock moves for either reason: no cursor to reach a side rail with, or no width to spare it.
  // A phone answers both in portrait and only the first one turned sideways, which is why the query
  // is not width alone — rotating one used to send the dock back to the left edge.
  const footDock = mediaBody("(hover: none), (max-width: 540px)");

  it("replaces the foot dock with the floating mark", () => {
    // contents, never a box: the dock is a child of the workspace grid, and a box there takes a row
    // from the reader — everything the dock holds is fixed, so the row it took measured zero and the
    // grid handed it half the window anyway.
    expect(footDock).toMatch(/\.mobile-dock\s*\{[^}]*display:\s*contents/);
    expect(footDock).toMatch(/\.sidebar\s*\{[^}]*display:\s*none/);
    // The mark's fan is the dock's buttons; the dock's strip goes back to the reader.
    expect(footDock).toMatch(/--foot-dock:\s*0px/);
    // railAnchor places the flyouts and has to ask exactly the same question.
    expect(railAnchor).toContain('"(hover: none), (max-width: 540px)"');
  });

  it("hides the mark everywhere the side rail is reachable", () => {
    expect(ruleBody(".mobile-dock")).toMatch(/display:\s*none/);
  });

  // The fan opens over the prose, where the panel surface is a hair from the page behind it and a
  // muted glyph on it has to be found among the words. The ink disc (variant 9) inverts instead, and
  // the two tokens swap themselves between the themes — black on white one way, white on black the
  // other — which is the whole reason it is written as a pair and not as two colours.
  it("draws the mark and its fan as ink discs, not bordered panels", () => {
    const disc = ruleBody(".mobile-dock-fan-btn");
    expect(disc).toMatch(/background:\s*var\(--text\)/);
    expect(disc).toMatch(/color:\s*var\(--bg\)/);
    expect(disc).toMatch(/border:\s*0/);
    // The mark is the same disc: it floats on the same prose, and the brand tile it carries draws
    // the inverted ground itself, so a panel-surfaced mark put a pale square inside a ring.
    const mark = ruleBody(".mobile-dock-fab");
    expect(mark).toMatch(/background:\s*var\(--text\)/);
    expect(mark).toMatch(/border:\s*0/);
    // And only the letter stands on it: the tile the brand mark draws it on is blended into the
    // disc, since a square inside the circle is a second shape rather than a mark.
    expect(ruleBody(".mobile-dock-mark.theme-asset-light")).toMatch(/mix-blend-mode:\s*lighten/);
    expect(ruleBody(".mobile-dock-mark.theme-asset-dark")).toMatch(/mix-blend-mode:\s*darken/);
    // Nothing on the disc can ink further, so being aimed at rings it instead.
    expect(ruleBody(".mobile-dock-fan-btn:focus-visible")).toMatch(/outline:[^;]*var\(--mark\)/);
  });

  // The dock's height is one measurement. Everything pinned to the bottom corner adds it, which is
  // why none of them needs a media query of its own — the token is zero while the dock is vertical,
  // and stays zero now that the dock is a floating mark instead of a strip.
  it("keeps the pinned chrome off the dock from a single measurement", () => {
    expect(ruleBody(":root")).toMatch(/--foot-dock:\s*0px/);

    for (const selector of [
      ".graph-panel",
      ".graph-fab",
      ".notification-toast",
      // The full-bleed graph fills the reader, which the dock floats over.
      ".graph-full .graph-controls",
      ".graph-full .graph-scope",
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

describe("docked aside", () => {
  // The rail is the window's height, not its content's — that is what puts the graph at its foot on a
  // note whose sections come up short, and what the list allocation measures against.
  it("sizes the rail from the window rather than its sections", () => {
    expect(mediaBody("(min-width: 1100px)")).toMatch(/> \.note-aside \{[^}]*height:\s*calc\(100vh/);
  });

  // Pushed to the foot when the sections above come up short, held there when they overflow. The
  // ground is what keeps the rows from reading through it; the rule is the edge they vanish under.
  it("holds the graph at the rail's foot, on ground of its own", () => {
    const pinned = mediaBody("(min-width: 1100px) and (min-height: 600px)");

    expect(pinned).toMatch(/margin-top:\s*auto/);
    expect(pinned).toMatch(/position:\s*sticky/);
    expect(pinned).toMatch(/bottom:\s*0/);
    expect(pinned).toMatch(/background:\s*var\(--panel\)/);
    // A short window keeps none of it: 280px of graph is the whole rail there.
    expect(mediaBody("(min-width: 1100px)")).not.toMatch(/position:\s*sticky[^}]*bottom:\s*0/);
  });
});

describe("pointers that cannot hover", () => {
  const touch = mediaBody("(hover: none)");

  it("opens none of the surfaces hover opens", () => {
    expect(touch).toMatch(/\.tab-tools\s*\{[^}]*display:\s*none/);
    expect(touch).toMatch(/\.rail-tip\s*\{[^}]*display:\s*none/);
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

describe("rough line marks", () => {
  it("scrolls with the prose instead of holding a fixed height", () => {
    // Sticky is what pinned the old single read-out to the top of the viewport; the marks belong to
    // the passages beside them now, and the strip is only their origin.
    expect(ruleBody(".line-hint")).toMatch(/position:\s*relative/);
    expect(ruleBody(".line-hint")).not.toMatch(/position:\s*sticky/);
    // Each mark is placed by the component (style.top); a rule that fixed it here would stack them.
    expect(ruleBody(".line-hint-mark")).not.toMatch(/top:/);
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

describe("floating preview", () => {
  it("reads previewed prose in body ink, not the chrome's muted ink", () => {
    expect(ruleBody(".wiki-preview")).toMatch(/color:\s*var\(--text\)/);
    expect(ruleBody(".wiki-preview-body .markdown-view p")).not.toMatch(/color:/);
  });
});

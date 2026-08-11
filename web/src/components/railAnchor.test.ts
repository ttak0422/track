import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { railAnchor } from "./railAnchor";

beforeAll(() => {
  window.innerHeight = 800;
});

afterEach(() => {
  vi.unstubAllGlobals();
});

// A rail button at the given vertical band of an 800px-tall viewport.
function button(top: number, height = 40): HTMLElement {
  const el = document.createElement("button");
  el.getBoundingClientRect = () =>
    ({ top, bottom: top + height, left: 8, right: 48 }) as DOMRect;
  return el;
}

describe("railAnchor", () => {
  it("hangs a flyout from a high button's top edge, beside the rail", () => {
    expect(railAnchor(button(120))).toEqual({ top: 120, left: 60 });
  });

  it("raises it from the foot of a button low enough to send the panel off screen", () => {
    // Settings is pinned to the bottom of the dock: 600 + a 230px panel would run past 800.
    expect(railAnchor(button(600))).toEqual({ bottom: 800 - 640, left: 60 });
  });

  // At phone width the dock lies along the foot of the window: there is nothing beside a button but
  // the next button, and a button near the right edge would send its panel off screen.
  it("raises a flyout off the foot dock at phone width", () => {
    vi.stubGlobal(
      "matchMedia",
      vi.fn((query: string) => ({ matches: query === "(max-width: 540px)" })),
    );

    expect(railAnchor(button(742))).toEqual({ left: 8, bottom: 800 - 742 + 8 });
  });

  it("has no placement for a trigger that is not mounted", () => {
    expect(railAnchor(null)).toBeUndefined();
  });
});

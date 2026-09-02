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

  // The phone's trigger is the floating mark, which is dragged wherever the reader likes, so a panel
  // placed from its rect landed somewhere new every time. It takes the window instead — one place,
  // whatever corner the mark is resting in.
  it("gives the phone's flyout the window, not a spot beside the trigger", () => {
    vi.stubGlobal(
      "matchMedia",
      vi.fn((query: string) => ({ matches: query === "(hover: none), (max-width: 540px)" })),
    );

    expect(railAnchor(button(742))).toEqual({ inset: 8 });
    // Wherever the mark has been dragged to, the same box.
    expect(railAnchor(button(120))).toEqual({ inset: 8 });
  });

  it("has no placement for a trigger that is not mounted", () => {
    expect(railAnchor(null)).toBeUndefined();
  });
});

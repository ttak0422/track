import { describe, expect, it } from "vitest";
import { isZoomWheel } from "./graphWheel";

describe("isZoomWheel", () => {
  it("zooms on a trackpad pinch (ctrl+wheel)", () => {
    expect(isZoomWheel({ ctrlKey: true, deltaMode: 0, deltaX: 0, deltaY: 4 })).toBe(true);
  });

  it("zooms on a line/page-mode mouse wheel", () => {
    expect(isZoomWheel({ ctrlKey: false, deltaMode: 1, deltaX: 0, deltaY: 3 })).toBe(true);
  });

  it("zooms on a big quantized vertical mouse-wheel notch", () => {
    expect(isZoomWheel({ ctrlKey: false, deltaMode: 0, deltaX: 0, deltaY: 100 })).toBe(true);
    expect(isZoomWheel({ ctrlKey: false, deltaMode: 0, deltaX: 0, deltaY: -120 })).toBe(true);
  });

  it("leaves a trackpad two-finger scroll to the page", () => {
    // small vertical
    expect(isZoomWheel({ ctrlKey: false, deltaMode: 0, deltaX: 0, deltaY: 8 })).toBe(false);
    // fractional (momentum)
    expect(isZoomWheel({ ctrlKey: false, deltaMode: 0, deltaX: 0, deltaY: 62.5 })).toBe(false);
    // diagonal scroll carries a horizontal component
    expect(isZoomWheel({ ctrlKey: false, deltaMode: 0, deltaX: 4, deltaY: 90 })).toBe(false);
  });
});

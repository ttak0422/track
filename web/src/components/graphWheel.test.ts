import { describe, expect, it } from "vitest";
import { isZoomWheel, zoomDelta } from "./graphWheel";

const wheel = { ctrlKey: false, shiftKey: false, deltaMode: 0, deltaX: 0, deltaY: 0 };

describe("isZoomWheel", () => {
  it("zooms on a trackpad pinch (ctrl+wheel)", () => {
    expect(isZoomWheel({ ...wheel, ctrlKey: true, deltaY: 4 })).toBe(true);
  });

  it("zooms on shift+wheel, even as the horizontal delta browsers turn it into", () => {
    expect(isZoomWheel({ ...wheel, shiftKey: true, deltaY: 8 })).toBe(true);
    expect(isZoomWheel({ ...wheel, shiftKey: true, deltaX: 120 })).toBe(true);
  });

  it("zooms on a line/page-mode mouse wheel", () => {
    expect(isZoomWheel({ ...wheel, deltaMode: 1, deltaY: 3 })).toBe(true);
  });

  it("zooms on a big quantized vertical mouse-wheel notch", () => {
    expect(isZoomWheel({ ...wheel, deltaY: 100 })).toBe(true);
    expect(isZoomWheel({ ...wheel, deltaY: -120 })).toBe(true);
  });

  it("leaves a trackpad two-finger scroll to the page", () => {
    // small vertical
    expect(isZoomWheel({ ...wheel, deltaY: 8 })).toBe(false);
    // fractional (momentum)
    expect(isZoomWheel({ ...wheel, deltaY: 62.5 })).toBe(false);
    // diagonal scroll carries a horizontal component
    expect(isZoomWheel({ ...wheel, deltaX: 4, deltaY: 90 })).toBe(false);
  });

  it("with needsModifier, only a modifier zooms — a bare wheel always scrolls", () => {
    expect(isZoomWheel({ ...wheel, deltaY: -120 }, true)).toBe(false);
    expect(isZoomWheel({ ...wheel, deltaMode: 1, deltaY: 3 }, true)).toBe(false);
    expect(isZoomWheel({ ...wheel, ctrlKey: true, deltaY: 4 }, true)).toBe(true);
    expect(isZoomWheel({ ...wheel, shiftKey: true, deltaX: 120 }, true)).toBe(true);
  });
});

describe("zoomDelta", () => {
  it("reads the vertical delta, falling back to the horizontal one shift+wheel produces", () => {
    expect(zoomDelta({ ...wheel, deltaY: -120 })).toBe(-120);
    expect(zoomDelta({ ...wheel, shiftKey: true, deltaX: -120 })).toBe(-120);
  });
});

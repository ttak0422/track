import { describe, expect, it } from "vitest";
import { isZoomWheel, zoomDelta } from "./graphWheel";

const wheel = { ctrlKey: false, shiftKey: false, deltaX: 0, deltaY: 0 };

describe("isZoomWheel", () => {
  it("zooms on a trackpad pinch (ctrl+wheel)", () => {
    expect(isZoomWheel({ ...wheel, ctrlKey: true, deltaY: 4 })).toBe(true);
  });

  it("zooms on shift+wheel, even as the horizontal delta browsers turn it into", () => {
    expect(isZoomWheel({ ...wheel, shiftKey: true, deltaY: 8 })).toBe(true);
    expect(isZoomWheel({ ...wheel, shiftKey: true, deltaX: 120 })).toBe(true);
  });

  it("leaves a bare wheel alone, whatever the device looks like", () => {
    expect(isZoomWheel({ ...wheel, deltaY: 8 })).toBe(false); // trackpad scroll
    expect(isZoomWheel({ ...wheel, deltaY: -120 })).toBe(false); // mouse notch
    expect(isZoomWheel({ ...wheel, deltaX: 4, deltaY: 90 })).toBe(false); // diagonal
  });
});

describe("zoomDelta", () => {
  it("reads the vertical delta, falling back to the horizontal one shift+wheel produces", () => {
    expect(zoomDelta({ ...wheel, deltaY: -120 })).toBe(-120);
    expect(zoomDelta({ ...wheel, shiftKey: true, deltaX: -120 })).toBe(-120);
  });
});

import { beforeEach, describe, expect, it } from "vitest";
import {
  clamp,
  constrainPreviewBounds,
  initialPreviewBounds,
  type PreviewAnchor,
  resizePreviewBounds,
} from "./bounds";

function setViewport(width: number, height: number) {
  window.innerWidth = width;
  window.innerHeight = height;
}

beforeEach(() => {
  setViewport(1200, 800);
});

describe("clamp", () => {
  it("bounds a value to [min, max]", () => {
    expect(clamp(5, 0, 10)).toBe(5);
    expect(clamp(-1, 0, 10)).toBe(0);
    expect(clamp(11, 0, 10)).toBe(10);
  });
});

describe("constrainPreviewBounds", () => {
  it("keeps an in-range box unchanged", () => {
    const box = { left: 100, top: 100, width: 400, height: 300 };
    expect(constrainPreviewBounds(box)).toEqual(box);
  });
  it("clamps an oversized off-screen box back into the viewport", () => {
    const out = constrainPreviewBounds({ left: -100, top: -100, width: 5000, height: 5000 });
    expect(out).toEqual({ left: -100, top: -20, width: 1176, height: 776 });
  });
  it("allows a dragged preview to sit mostly off-screen while leaving a visible strip", () => {
    expect(constrainPreviewBounds({ left: -1000, top: 100, width: 400, height: 300 }).left).toBe(-356);
    expect(constrainPreviewBounds({ left: 2000, top: 100, width: 400, height: 300 }).left).toBe(1156);
    expect(constrainPreviewBounds({ left: 100, top: 2000, width: 400, height: 300 }).top).toBe(756);
  });
});

describe("initialPreviewBounds", () => {
  const anchor = (over: Partial<PreviewAnchor>): PreviewAnchor => ({
    linkLeft: 100,
    linkRight: 200,
    linkTop: 100,
    linkBottom: 120,
    ...over,
  });

  it("places the preview to the right of the link when there is room", () => {
    const b = initialPreviewBounds(anchor({}));
    expect(b.left).toBe(212); // linkRight + previewSideGap
    expect(b.top).toBe(100); // linkTop
  });

  it("falls back to the left of the link when the right has no room", () => {
    const b = initialPreviewBounds(anchor({ linkLeft: 1000, linkRight: 1100 }));
    expect(b.left).toBe(388); // linkLeft - previewSideGap - width(600)
    expect(b.top).toBe(100);
  });

  it("falls back to below the link on a narrow viewport", () => {
    setViewport(600, 800);
    const b = initialPreviewBounds(anchor({ linkLeft: 250, linkRight: 350 }));
    expect(b.left).toBe(250); // linkLeft
    expect(b.top).toBe(128); // linkBottom + 8
  });

  it("scales the opening height with the viewport height (width unchanged)", () => {
    setViewport(1200, 800);
    const short = initialPreviewBounds(anchor({}));
    expect(short.height).toBe(560); // 0.7 * 800
    setViewport(1200, 1200);
    const tall = initialPreviewBounds(anchor({}));
    expect(tall.height).toBe(840); // 0.7 * 1200
    expect(tall.width).toBe(short.width); // height change leaves width alone
  });

  it("floors the opening height at the previous default on a short viewport", () => {
    setViewport(1200, 396); // 0.7 * 396 ≈ 277 < 280, and there is room for 280 below the link
    expect(initialPreviewBounds(anchor({})).height).toBe(280);
  });
});

describe("resizePreviewBounds", () => {
  const start = { left: 100, top: 100, width: 400, height: 300 };

  it("grows from the south-east corner", () => {
    expect(resizePreviewBounds("se", start, 50, 20)).toEqual({
      left: 100,
      top: 100,
      width: 450,
      height: 320,
    });
  });

  it("moves the origin when dragging the north-west corner", () => {
    expect(resizePreviewBounds("nw", start, 50, 50)).toEqual({
      left: 150,
      top: 150,
      width: 350,
      height: 250,
    });
  });
});

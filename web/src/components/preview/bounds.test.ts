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
    expect(out).toEqual({ left: 12, top: 12, width: 1176, height: 776 });
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

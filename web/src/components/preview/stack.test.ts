import { afterEach, describe, expect, it } from "vitest";
import {
  bringPreviewToFront,
  getPreviewStackOrder,
  previewMaxZIndex,
  previewBaseZIndex,
  registerPreview,
  releasePreview,
} from "./stack";

describe("preview stack", () => {
  let ids: string[] = [];

  afterEach(() => {
    for (const id of ids) releasePreview(id);
    ids = [];
  });

  it("renormalizes repeated raises inside the preview band", () => {
    ids = ["first", "second"];
    registerPreview("first");
    registerPreview("second");

    for (let i = 0; i < 1000; i += 1) bringPreviewToFront("first");

    expect(previewBaseZIndex + getPreviewStackOrder("first")).toBeLessThanOrEqual(previewMaxZIndex);
    expect(previewBaseZIndex + getPreviewStackOrder("second")).toBeLessThan(
      previewBaseZIndex + getPreviewStackOrder("first"),
    );
  });

  // The order is flat: what a window was opened from has no bearing on where it sits. A window
  // opened later starts in front, and raising an older one puts it back on top of the newer.
  it("orders by last activation, not by who opened what", () => {
    ids = ["parent", "child"];
    registerPreview("parent");
    registerPreview("child");
    expect(getPreviewStackOrder("child")).toBeGreaterThan(getPreviewStackOrder("parent"));

    bringPreviewToFront("parent");
    expect(getPreviewStackOrder("parent")).toBeGreaterThan(getPreviewStackOrder("child"));
  });

  // Closing one window leaves the rest exactly as they were relative to each other.
  it("keeps the remaining order when a window in the middle closes", () => {
    ids = ["a", "c"];
    registerPreview("a");
    registerPreview("b");
    registerPreview("c");

    releasePreview("b");

    expect(getPreviewStackOrder("c")).toBeGreaterThan(getPreviewStackOrder("a"));
  });
});

import { afterEach, describe, expect, it } from "vitest";
import {
  bringPreviewToFront,
  createPreviewID,
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
    const first = createPreviewID();
    const second = createPreviewID();
    ids = [first, second];
    registerPreview(first);
    registerPreview(second);

    for (let i = 0; i < 1000; i += 1) bringPreviewToFront(first);

    expect(previewBaseZIndex + getPreviewStackOrder(first)).toBeLessThanOrEqual(previewMaxZIndex);
    expect(previewBaseZIndex + getPreviewStackOrder(second)).toBeLessThan(
      previewBaseZIndex + getPreviewStackOrder(first),
    );
  });
});

import { describe, expect, it } from "vitest";
import { lineAtViewportTop, roughLineLabel, type LineHintSpan } from "./roughLine";

describe("roughLineLabel", () => {
  it("rounds down to the hundred-line band the position falls in", () => {
    expect(roughLineLabel(432)).toBe("~400");
    expect(roughLineLabel(999)).toBe("~900");
    // An exact multiple is already at its own band's head.
    expect(roughLineLabel(1000)).toBe("~1000");
  });

  it("stays quiet over the first band and unknown positions", () => {
    expect(roughLineLabel(1)).toBeNull();
    expect(roughLineLabel(99)).toBeNull();
    // Line 100 is already the head of the second band, so it names itself.
    expect(roughLineLabel(100)).toBe("~100");
    expect(roughLineLabel(null)).toBeNull();
    expect(roughLineLabel(0)).toBeNull();
  });

  it("honours a custom unit", () => {
    expect(roughLineLabel(432, 100)).toBe("~400");
    expect(roughLineLabel(432, 10)).toBe("~430");
    expect(roughLineLabel(9, 10)).toBeNull();
  });
});

describe("lineAtViewportTop", () => {
  const spans: LineHintSpan[] = [
    { start: 1, top: -300 },
    { start: 120, top: 40 },
    { start: 340, top: 500 },
  ];

  it("picks the last block whose top has reached the viewport edge", () => {
    expect(lineAtViewportTop(spans, -100)).toBe(1);
    expect(lineAtViewportTop(spans, 40)).toBe(120);
    expect(lineAtViewportTop(spans, 900)).toBe(340);
  });

  it("reads a gap between blocks as the block above it", () => {
    expect(lineAtViewportTop(spans, 200)).toBe(120);
  });

  it("falls back to the first block when scrolled above all content", () => {
    expect(lineAtViewportTop(spans, -800)).toBe(1);
  });

  it("says nothing without stamped blocks", () => {
    expect(lineAtViewportTop([], 0)).toBeNull();
  });
});

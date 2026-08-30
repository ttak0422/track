import { describe, expect, it } from "vitest";
import { bandMarks, roughLineLabel, type LineHintSpan } from "./roughLine";

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

describe("bandMarks", () => {
  const spans: LineHintSpan[] = [
    { start: 1, top: 0 },
    { start: 40, top: 90 },
    { start: 120, top: 300 },
    { start: 180, top: 520 },
    { start: 340, top: 900 },
  ];

  it("names each band once, at the block that opens it", () => {
    expect(bandMarks(spans)).toEqual([
      { top: 300, label: "~100" },
      { top: 900, label: "~300" },
    ]);
  });

  // The first band has no number to show, so a note that never leaves it shows nothing — the same
  // silence roughLineLabel keeps, not a mark at the top of every note.
  it("stays quiet over a note that never leaves the first band", () => {
    expect(bandMarks([{ start: 1, top: 0 }, { start: 60, top: 200 }])).toEqual([]);
  });

  it("says nothing without stamped blocks", () => {
    expect(bandMarks([])).toEqual([]);
  });

  // An included excerpt can run the numbers backwards; the mark describes the block beside it, so
  // the band is named again where it comes back.
  it("names a band again when the blocks return to it", () => {
    const included: LineHintSpan[] = [
      { start: 120, top: 100 },
      { start: 520, top: 400 },
      { start: 140, top: 700 },
    ];
    expect(bandMarks(included)).toEqual([
      { top: 100, label: "~100" },
      { top: 400, label: "~500" },
      { top: 700, label: "~100" },
    ]);
  });

  it("honours a custom unit", () => {
    expect(bandMarks([{ start: 12, top: 0 }, { start: 24, top: 50 }], 10)).toEqual([
      { top: 0, label: "~10" },
      { top: 50, label: "~20" },
    ]);
  });
});

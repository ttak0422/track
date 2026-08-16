import { describe, expect, it } from "vitest";
import { radiusForNode } from "./nodeRadius";

describe("radiusForNode", () => {
  it("keeps the centre focal, and never smaller than its own grade", () => {
    expect(radiusForNode({ note_id: "c", file_kind: "note", title: "", center: true, size: 1 }, "c")).toBe(10);
    expect(radiusForNode({ note_id: "c", file_kind: "note", title: "", size: 5 }, "c")).toBe(17);
  });

  it("draws the precomputed grade: five levels from stub to hub", () => {
    const radius = (size: number) => radiusForNode({ note_id: "n", file_kind: "note", title: "", size }, "c");
    expect(radius(1)).toBe(4);
    expect(radius(5)).toBe(17);
    // Monotonic across the five levels.
    const radii = [1, 2, 3, 4, 5].map(radius);
    expect([...radii].sort((a, b) => a - b)).toEqual(radii);
    // A grade apart is a gap you can see: every step is at least a third larger than the last.
    for (let i = 1; i < radii.length; i++) expect(radii[i]).toBeGreaterThan(radii[i - 1] * 1.33);
  });

  it("falls back to the degree-based size when no grade rides along", () => {
    const none = radiusForNode({ note_id: "n", file_kind: "note", title: "", degree: 0 }, "c");
    const hub = radiusForNode({ note_id: "n", file_kind: "note", title: "", degree: 20 }, "c");
    expect(hub).toBeGreaterThan(none);
  });

  it("ignores an out-of-range grade", () => {
    expect(radiusForNode({ note_id: "n", file_kind: "note", title: "", size: 0 }, "c")).toBe(6);
    expect(radiusForNode({ note_id: "n", file_kind: "note", title: "", size: 9 }, "c")).toBe(6);
  });
});

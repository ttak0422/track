import { describe, expect, it } from "vitest";
import { radiusForNode } from "./nodeRadius";

describe("radiusForNode", () => {
  it("keeps the centre focal, and never smaller than its own score", () => {
    expect(radiusForNode({ note_id: "c", file_kind: "note", title: "", center: true, size: 1 }, "c")).toBe(10);
    expect(radiusForNode({ note_id: "c", file_kind: "note", title: "", size: 5 }, "c")).toBe(17);
  });

  it("maps continuous area scores to bounded radii", () => {
    const radius = (area_score: number) => radiusForNode({ note_id: "n", file_kind: "note", title: "", area_score }, "c");
    expect(radius(1)).toBe(4);
    expect(radius(5)).toBe(17);
    // Fractional scores survive JSON and remain continuous.
    const radii = [1, 1.01, 1.8, 2.25, 3, 4.9, 5].map(radius);
    expect([...radii].sort((a, b) => a - b)).toEqual(radii);
    expect(radius(3) ** 2).toBeCloseTo((4 ** 2 + 17 ** 2) / 2);
    expect(radius(1.81) - radius(1.8)).toBeLessThan(0.1);
  });

  it("falls back to the degree-based size when no score rides along", () => {
    const none = radiusForNode({ note_id: "n", file_kind: "note", title: "", degree: 0 }, "c");
    const hub = radiusForNode({ note_id: "n", file_kind: "note", title: "", degree: 20 }, "c");
    expect(hub).toBeGreaterThan(none);
  });

  it("ignores an out-of-range or non-finite score", () => {
    expect(radiusForNode({ note_id: "n", file_kind: "note", title: "", size: 0 }, "c")).toBe(6);
    for (const area_score of [0, 9, NaN, Infinity]) {
      expect(radiusForNode({ note_id: "n", file_kind: "note", title: "", size: 3, area_score }, "c")).toBe(8.5);
    }
  });
});

// Old and new payloads work regardless of which side is updated first.
describe("graph size compatibility", () => {
  const node = { note_id: "n", file_kind: "note", title: "" };
  // The previous client's grade lookup, unchanged by an additive field it does not read.
  const oldClient = (payload: { size: number }) => [4, 6, 8.5, 12, 17][payload.size - 1];
  it("preserves every legacy radius when the server is still old", () => {
    for (const size of [1,2,3,4,5]) {
      expect(radiusForNode({ ...node, size }, "c")).toBe(oldClient({size}));
    }
    expect(radiusForNode({ ...node, degree: 20 }, "n")).toBe(10);
  });
  it("keeps an old client on its original grade when the server updates first", () => {
    const payload = JSON.parse('{"size":2,"area_score":1.8}');
    expect(oldClient(payload)).toBe(6);
    expect(radiusForNode({ ...node, ...payload }, "c")).toBeCloseTo(8.40238);
  });
});

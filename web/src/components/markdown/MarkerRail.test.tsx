import { describe, expect, it } from "vitest";
import {
  BOX_WIDTH,
  computeRailLayout,
  extractRail,
  LANE_HEIGHT,
  layoutLanes,
  RAIL_TOP,
  railSide,
} from "./MarkerRail";

describe("extractRail", () => {
  const option = (): Record<string, unknown> => ({
    xAxis: { type: "category", data: ["a", "b", "c", "d"] },
    series: [
      {
        markLine: {
          data: [
            { xAxis: "b", label: { formatter: "ev" }, href: "https://example.com/x", box: { date: "2026-01-02", host: "example.com" } },
            { xAxis: "d", box: { date: "d" } },
            { xAxis: "a", label: { formatter: "classic marker" } }, // no box payload: not on the rail
            { yAxis: 5 }, // reference line
          ],
        },
      },
    ],
  });

  it("collects box-mode items in emitted order with their axis fractions", () => {
    const rail = extractRail(option());
    expect(rail.boxes.map((b) => b.at)).toEqual(["b", "d"]);
    expect(rail.boxes[0]).toEqual({
      at: "b",
      date: "2026-01-02",
      headline: "ev",
      href: "https://example.com/x",
      host: "example.com",
    });
    expect(rail.boxes[1].headline).toBe("");
    expect(rail.fractions).toEqual([1.5 / 4, 3.5 / 4]);
  });

  it("drops non-http(s) hrefs even if a foreign option carries them", () => {
    const opt = option();
    const item = (
      (opt.series as { markLine: { data: Record<string, unknown>[] } }[])[0].markLine.data
    )[0];
    item.href = "javascript:alert(1)";
    const rail = extractRail(opt);
    expect(rail.boxes[0].href).toBe("");
  });

  it("returns nothing for options without box markers", () => {
    expect(extractRail({ series: [{ data: [1, 2] }] }).boxes).toHaveLength(0);
    expect(extractRail({}).boxes).toHaveLength(0);
  });
});

describe("railSide", () => {
  it("alternates boxes across the chart so same-day pairs split sides", () => {
    expect([0, 1, 2, 3].map(railSide)).toEqual(["below", "above", "below", "above"]);
  });
});

describe("computeRailLayout", () => {
  it("splits boxes across the bands and settles their heights from width alone", () => {
    // 4 boxes spread over the axis: 2 per side, one lane each on a wide container.
    const layout = computeRailLayout(4, [0.1, 0.35, 0.6, 0.85], 1000);
    expect(layout.mode).toBe("rail");
    expect(layout.below.indexes).toEqual([0, 2]);
    expect(layout.above.indexes).toEqual([1, 3]);
    expect(layout.below.height).toBe(LANE_HEIGHT + RAIL_TOP);
    expect(layout.above.height).toBe(LANE_HEIGHT + RAIL_TOP);
  });

  it("flips to list mode on narrow containers and reports empty sides as zero height", () => {
    expect(computeRailLayout(4, [0.1, 0.35, 0.6, 0.85], 400).mode).toBe("list");
    const single = computeRailLayout(1, [0.5], 1000);
    expect(single.above.height).toBe(0);
    expect(single.below.height).toBe(LANE_HEIGHT + RAIL_TOP);
  });
});

describe("layoutLanes", () => {
  it("stacks overlapping boxes into successive lanes and reuses freed lanes", () => {
    // Two boxes at the same anchor collide; a third far away fits back on lane 0.
    const { slots, lanes } = layoutLanes([100, 110, 600], 800);
    expect(slots[0].lane).toBe(0);
    expect(slots[1].lane).toBe(1);
    expect(slots[2].lane).toBe(0);
    expect(lanes).toBe(2);
  });

  it("clamps boxes into the rail at both edges", () => {
    const { slots } = layoutLanes([0, 800], 800);
    expect(slots[0].left).toBe(0);
    expect(slots[1].left).toBe(800 - BOX_WIDTH);
  });
});

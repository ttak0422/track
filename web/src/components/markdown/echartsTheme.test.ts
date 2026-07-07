import { describe, expect, it } from "vitest";
import { applyChartTheme, type ChartTheme } from "./echartsTheme";

const THEME: ChartTheme = {
  text: "#111111",
  muted: "#666666",
  line: "#dddddd",
  panel: "#ffffff",
  danger: "#aa0000",
  palette: ["#010101", "#020202"],
  rampLo: "#eeeeee",
  rampHi: "#000033",
};

describe("applyChartTheme", () => {
  it("recolors palette, text, axes, and overlay geometry without mutating the input", () => {
    const option = {
      color: ["#4e79a7"],
      title: { text: "t", left: 16 },
      legend: { data: ["a"] },
      xAxis: { type: "category" },
      yAxis: [{ type: "value", splitLine: { show: false } }],
      tooltip: { trigger: "axis" },
      series: [
        {
          type: "line",
          areaStyle: { color: "rgba(78,121,167,0.3)" },
          markLine: {
            lineStyle: { color: "red" },
            label: { color: "red" },
            data: [{ xAxis: "x", label: { backgroundColor: "rgba(255,255,255,0.8)" } }],
          },
          markPoint: { itemStyle: { color: "red" }, label: { backgroundColor: "#ffffff" } },
        },
      ],
    };
    const before = JSON.stringify(option);

    const themed = applyChartTheme(option, THEME) as any;

    expect(JSON.stringify(option)).toBe(before);
    expect(themed.color).toEqual(THEME.palette);
    expect(themed.title.textStyle.color).toBe(THEME.text);
    expect(themed.title.left).toBe(16);
    expect(themed.legend.textStyle.color).toBe(THEME.text);
    expect(themed.xAxis.axisLabel.color).toBe(THEME.muted);
    // Merged, not clobbered: the y-axis keeps its splitLine.show flag.
    expect(themed.yAxis[0].splitLine).toMatchObject({ show: false, lineStyle: { color: THEME.line } });
    expect(themed.tooltip.backgroundColor).toBe(THEME.panel);
    const s = themed.series[0];
    expect(s.areaStyle.color).toBe("rgba(1,1,1,0.3)");
    expect(s.markLine.label.color).toBe(THEME.danger);
    expect(s.markLine.data[0].label.backgroundColor).toBe("rgba(255,255,255,0.85)");
    expect(s.markPoint.label.borderColor).toBe(THEME.danger);
  });

  it("swaps the heatmap ramp only when one exists", () => {
    const themed = applyChartTheme(
      { visualMap: { inRange: { color: ["#a", "#b"] } } },
      THEME,
    ) as any;
    expect(themed.visualMap.inRange.color).toEqual([THEME.rampLo, THEME.rampHi]);
  });

  it("repaints treemap gaps and group headings to the theme surface", () => {
    const themed = applyChartTheme(
      {
        series: [
          {
            type: "treemap",
            upperLabel: { show: true, height: 20 },
            levels: [
              { itemStyle: { gapWidth: 2, borderWidth: 2, borderColor: "#ffffff" } },
              { itemStyle: { gapWidth: 1 } },
            ],
          },
        ],
      },
      THEME,
    ) as any;
    const s = themed.series[0];
    // Every level's border becomes the panel surface — the group bands are ECharts' *default* white
    // border on the un-styled level, so the defaulted level must be painted too.
    expect(s.levels[0].itemStyle.borderColor).toBe(THEME.panel);
    expect(s.levels[1].itemStyle.borderColor).toBe(THEME.panel);
    expect(s.upperLabel.color).toBe(THEME.text);
  });

  it("keeps the diverging ramp's market endpoints and themes only its neutral", () => {
    const themed = applyChartTheme(
      { visualMap: { inRange: { color: ["#e15759", "#f5f5f5", "#59a14f"] } } },
      THEME,
    ) as any;
    expect(themed.visualMap.inRange.color).toEqual(["#e15759", THEME.panel, "#59a14f"]);
  });
});

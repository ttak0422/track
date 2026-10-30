import { describe, expect, it } from "vitest";
import { applyChartTheme, chartSeriesKey, chartThemeFromCSS, type ChartTheme } from "./echartsTheme";

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

describe("chartThemeFromCSS", () => {
  it("keeps its fallback palette aligned with the quiet-sheet figure colors", () => {
    const theme = chartThemeFromCSS();

    expect(theme.palette).toEqual(["#303030", "#b23a22", "#005e9e", "#79538a", "#766000", "#00696b"]);
    expect(theme.rampLo).toBe("#e4ebe7");
    expect(theme.rampHi).toBe("#286957");
  });

  it("reads custom chart tokens instead of forcing the fallback palette", () => {
    document.documentElement.style.setProperty("--chart-1", "#123456");
    try {
      expect(chartThemeFromCSS().palette[0]).toBe("#123456");
    } finally {
      document.documentElement.style.removeProperty("--chart-1");
    }
  });
});

describe("applyChartTheme", () => {
  it("recolors palette, text, axes, and overlay geometry without mutating the input", () => {
    const option = {
      _trackTheme: true,
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
    expect(themed._trackTheme).toBeUndefined();
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

  it("backs a treemap's title and visualMap with an opaque chip so roamed tiles pass beneath", () => {
    const themed = applyChartTheme(
      {
        title: { text: "map" },
        visualMap: { inRange: { color: ["#eee", "#333"] } },
        series: [{ type: "treemap" }],
      },
      THEME,
    ) as any;
    expect(themed.title.backgroundColor).toBe(THEME.panel);
    expect(themed.visualMap.backgroundColor).toBe(THEME.panel);
  });

  it("keeps the diverging ramp's market endpoints and themes only its neutral", () => {
    const themed = applyChartTheme(
      { visualMap: { inRange: { color: ["#e15759", "#f5f5f5", "#59a14f"] } } },
      THEME,
    ) as any;
    expect(themed.visualMap.inRange.color).toEqual(["#e15759", THEME.panel, "#59a14f"]);
  });

  it("keeps six line identities distinct when every color is identical", () => {
    const option = { _trackTheme: true, color: ["old"], series: Array.from({ length: 6 }, (_, i) => ({
      type: "line", name: `Service ${i + 1}`, showSymbol: false, connectNulls: false, data: [1, null, 2],
    })) };
    const before = JSON.stringify(option);
    const themed = applyChartTheme(option, { ...THEME, palette: Array(6).fill("#555555") }) as any;
    const identities = themed.series.map((s: any) => `${s.lineStyle.type}/${s.symbol}`);
    expect(new Set(identities).size).toBe(6);
    for (const s of themed.series) {
      expect(s.showSymbol).toBe(true);
      expect(s.symbolSize).toBe(7);
      expect(s.lineStyle.width).toBe(3);
      expect(s.connectNulls).toBe(false);
      expect(s.data).toEqual([1, null, 2]);
    }
    expect(chartSeriesKey(option).slice(0, 2)).toEqual([
      "Service 1: solid line, circle markers", "Service 2: dashed line, diamond markers",
    ]);
    expect(JSON.stringify(option)).toBe(before);
  });

  it("preserves raw palette, line and marker overrides and explicit fill colors", () => {
    const option = { color: ["#123456"], series: [{ type: "line", name: "Custom", color: "#abcdef",
      lineStyle: { type: "dotted", width: 5 }, symbol: "triangle", showSymbol: false,
      areaStyle: { color: "#fedcba" }, data: [0, 1] }] };
    const themed = applyChartTheme(option, THEME) as any;
    expect(themed.color).toEqual(["#123456"]);
    expect(themed.series[0]).toMatchObject(option.series[0]);
    expect(chartSeriesKey({ series: [option.series[0], { type: "line", name: "Other" }] })[0])
      .toBe("Custom: dotted line, no markers");
  });

  it("retains generated per-series color and stroke overrides while adding missing cues", () => {
    const themed = applyChartTheme({ _trackTheme: true, series: [{ type: "line", color: "#123456",
      lineStyle: { type: "dotted", width: 4 }, areaStyle: { color: "old" } }] }, THEME) as any;
    expect(themed.series[0]).toMatchObject({ color: "#123456", lineStyle: { type: "dotted", width: 4 }, symbol: "circle" });
    expect(themed.series[0].areaStyle.color).toBe("rgba(18,52,86,0.3)");
    const withOpacity = applyChartTheme({ _trackTheme: true, series: [{ type: "line", color: "#123456",
      areaStyle: { color: "#123456", opacity: 0.6 } }] }, THEME) as any;
    expect(withOpacity.series[0].areaStyle).toMatchObject({ color: "#123456", opacity: 0.6 });
  });
});

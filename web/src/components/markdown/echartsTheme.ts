// echartsTheme recolors a server-resolved ECharts option to the app's current theme. The Go engine
// decides chart semantics and emits theme-neutral structure; this module owns presentation color so
// charts follow the light/dark theme like the rest of the page (the same split MermaidDiagram uses:
// CSS variables in, themed render out).

export interface ChartTheme {
  text: string;
  muted: string;
  line: string;
  panel: string;
  danger: string;
  palette: string[];
  rampLo: string;
  rampHi: string;
}

const PALETTE_FALLBACK = ["#2f6f5e", "#f08300", "#5b7aa5", "#8a352b", "#7d8a4e", "#96608f"];

// chartThemeFromCSS reads the theme variables off the document root, so the palette always matches
// whatever theme (light, dark, or a future override) is active at draw time.
export function chartThemeFromCSS(): ChartTheme {
  const css = getComputedStyle(document.documentElement);
  const v = (name: string, fallback: string) => css.getPropertyValue(name).trim() || fallback;
  return {
    text: v("--text", "#20231f"),
    muted: v("--muted", "#687069"),
    line: v("--line", "#d9ddd5"),
    panel: v("--panel", "#ffffff"),
    danger: v("--danger", "#8a352b"),
    palette: PALETTE_FALLBACK.map((fallback, i) => v(`--chart-${i + 1}`, fallback)),
    rampLo: v("--chart-ramp-lo", "#e3ece9"),
    rampHi: v("--chart-ramp-hi", "#174c40"),
  };
}

/* eslint-disable @typescript-eslint/no-explicit-any -- the option is free-form ECharts JSON */

// applyChartTheme returns a themed copy of the option (the input is left untouched — it may be a
// react-query cache entry). It restyles text, axes, tooltip, and overlay geometry, and swaps the
// series palette; semantic colors (candlestick up/down) stay as the engine chose them.
export function applyChartTheme(
  option: Record<string, unknown>,
  t: ChartTheme,
): Record<string, unknown> {
  const opt = structuredClone(option) as any;

  opt.color = t.palette;
  if (opt.title) {
    opt.title.textStyle = { ...opt.title.textStyle, color: t.text };
  }
  if (opt.legend) {
    opt.legend.textStyle = { ...opt.legend.textStyle, color: t.text };
  }
  for (const axis of [...asArray(opt.xAxis), ...asArray(opt.yAxis)]) {
    axis.axisLabel = { ...axis.axisLabel, color: t.muted };
    axis.axisLine = { ...axis.axisLine, lineStyle: { ...axis.axisLine?.lineStyle, color: t.line } };
    axis.splitLine = {
      ...axis.splitLine,
      lineStyle: { ...axis.splitLine?.lineStyle, color: t.line },
    };
  }
  if (opt.tooltip) {
    opt.tooltip.backgroundColor = t.panel;
    opt.tooltip.borderColor = t.line;
    opt.tooltip.textStyle = { ...opt.tooltip.textStyle, color: t.text };
  }
  if (opt.visualMap) {
    opt.visualMap.textStyle = { ...opt.visualMap.textStyle, color: t.muted };
    // The sequential (2-stop) ramp follows the theme wholesale. A diverging ramp (3 stops) keeps the
    // engine's semantic market red/green endpoints — like candlestick up/down — but its neutral
    // midpoint is presentation, not semantics: it becomes the panel surface so a near-zero cell
    // blends into the page in both themes instead of glowing near-white on dark.
    if (opt.visualMap.inRange?.color?.length === 2) {
      opt.visualMap.inRange.color = [t.rampLo, t.rampHi];
    } else if (opt.visualMap.inRange?.color?.length === 3) {
      opt.visualMap.inRange.color[1] = t.panel;
    }
  }
  for (const zoom of asArray(opt.dataZoom)) {
    zoom.textStyle = { ...zoom.textStyle, color: t.muted };
  }

  asArray(opt.series).forEach((s: any, i: number) => {
    if (s.areaStyle?.color) {
      s.areaStyle.color = withAlpha(t.palette[i % t.palette.length], 0.3);
    }
    if (s.markLine) {
      s.markLine.lineStyle = { ...s.markLine.lineStyle, color: withAlpha(t.danger, 0.7) };
      s.markLine.label = { ...s.markLine.label, color: t.danger };
      for (const item of asArray(s.markLine.data)) {
        if (item?.label?.backgroundColor) {
          item.label.backgroundColor = withAlpha(t.panel, 0.85);
        }
      }
    }
    if (s.markArea?.label) {
      s.markArea.label.color = t.muted;
    }
    if (s.markPoint) {
      s.markPoint.itemStyle = { ...s.markPoint.itemStyle, color: t.danger };
      s.markPoint.label = {
        ...s.markPoint.label,
        color: t.text,
        backgroundColor: t.panel,
        borderColor: t.danger,
      };
    }
    if (s.type === "treemap") {
      // The engine (and ECharts' defaults) paint the tile gaps, outer frame, and group-heading bands
      // white — right for the standalone page's white body, glaring in the reader's dark theme. The
      // bands are border area (upperLabel widens its level's top border), so every level's
      // borderColor becomes the panel surface, explicit or defaulted, and the headings become theme
      // text. The tiles' semantic market colors stay as the engine chose them.
      for (const level of asArray(s.levels)) {
        level.itemStyle = { ...level.itemStyle, borderColor: t.panel };
      }
      if (s.upperLabel) {
        s.upperLabel = { ...s.upperLabel, color: t.text };
      }
    }
  });

  return opt as Record<string, unknown>;
}

// asArray flattens ECharts' object-or-array option fields into a walkable list.
function asArray(value: unknown): any[] {
  if (Array.isArray(value)) {
    return value;
  }
  return value !== null && typeof value === "object" ? [value] : [];
}

// withAlpha turns "#rrggbb" into rgba at the given opacity; anything else passes through.
function withAlpha(hex: string, alpha: number): string {
  const m = /^#([0-9a-f]{6})$/i.exec(hex);
  if (!m) {
    return hex;
  }
  const v = parseInt(m[1], 16);
  return `rgba(${(v >> 16) & 0xff},${(v >> 8) & 0xff},${v & 0xff},${alpha})`;
}

/* eslint-enable @typescript-eslint/no-explicit-any */

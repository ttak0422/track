// echartsTheme recolors a server-resolved ECharts option to the app's current theme. The Go engine
// decides chart semantics and emits theme-neutral structure; this module owns presentation color so
// charts follow the light/dark theme like the rest of the page (the same split MermaidDiagram uses:
// CSS variables in, themed render out).

export interface ChartTheme {
  text: string;
  muted: string;
  line: string;
  // The surface used by things painted over the transparent plot (tooltip, label chips, treemap
  // gaps). The chart itself sits directly on the reading sheet.
  panel: string;
  danger: string;
  palette: string[];
  rampLo: string;
  rampHi: string;
}

// Keep the fallback aligned with styles.css: it is used by standalone tests and by browsers that do
// not expose a token (for example, before the stylesheet has loaded). The palette follows the current
// quiet-sheet design: ink and vermilion first, then supporting categorical colors. Color is never
// the only series key: line patterns, point shapes and the visible text key carry the same identity.
const PALETTE_FALLBACK = ["#303030", "#b23a22", "#005e9e", "#79538a", "#766000", "#00696b"];

const LINE_PATTERNS = ["solid", "dashed", "dotted"];
const LINE_SYMBOLS = ["circle", "diamond", "rect", "triangle", "roundRect", "pin"];
const SYMBOL_LABELS: Record<string, string> = { circle: "circle", diamond: "diamond", rect: "square",
  triangle: "triangle", roundRect: "rounded rectangle", pin: "pin" };

// Dashboard responses reserve slots using the full matched universe, including series outside
// the selected period. Other options use stable IDs/names sorted independently of array order.
// An unnamed series has no stable identity and retains its positional fallback.
function seriesSlots(option: Record<string, unknown>): number[] {
  const series = asArray(option.series);
  const identity = (s: any): string | undefined => {
    if (!s || typeof s !== "object") return undefined;
    if (typeof s.id === "string" && s.id) return `id:${s.id}`;
    if (typeof s.name === "string" && s.name) return `name:${s.name}`;
    return undefined;
  };
  const order = Array.isArray(option._trackSeriesOrder)
    ? option._trackSeriesOrder.filter((id): id is string => typeof id === "string").map((id) => `id:${id}`)
    : [...new Set(series.map(identity).filter((id): id is string => id !== undefined))].sort();
  const slots = new Map(order.map((id, i) => [id, i]));
  return series.map((s, i) => slots.get(identity(s) ?? "") ?? i);
}

function lineEncoding(series: any, index: number, trackTheme: boolean) {
  return {
    pattern: series.lineStyle?.type ?? LINE_PATTERNS[index % LINE_PATTERNS.length],
    symbol: series.symbol ?? LINE_SYMBOLS[index % LINE_SYMBOLS.length],
    // Generated options used to hide all vertices. A raw ECharts fence can still explicitly opt out.
    markers: (trackTheme || series.showSymbol !== false) && series.symbolSize !== 0 && series.symbol !== "none",
  };
}

// A plain-text counterpart to the chart legend remains useful without any color perception.
// It describes the actual defaults/overrides, rather than renaming the data series.
export function chartSeriesKey(option: Record<string, unknown>): string[] {
  const slots = seriesSlots(option);
  const keys = asArray(option.series).flatMap((s: any, i: number) => {
    if (!s || typeof s !== "object" || s.type !== "line" || typeof s.name !== "string" || !s.name) return [];
    const encoding = lineEncoding(s, slots[i], option._trackTheme === true);
    const pattern = typeof encoding.pattern === "string" ? encoding.pattern : "custom-pattern";
    const marker = encoding.markers && encoding.symbol !== "none"
      ? `${SYMBOL_LABELS[encoding.symbol] ?? "custom"} markers`
      : "no markers";
    return [`${s.name}: ${pattern} line, ${marker}`];
  });
  return keys.length > 1 ? keys : [];
}

// chartThemeFromCSS reads the theme variables off the document root, so the palette always matches
// whatever theme (light, dark, or a future override) is active at draw time.
export function chartThemeFromCSS(): ChartTheme {
  const css = getComputedStyle(document.documentElement);
  const v = (name: string, fallback: string) => css.getPropertyValue(name).trim() || fallback;
  return {
    text: v("--text", "#1a1a18"),
    muted: v("--muted", "#5e5d58"),
    line: v("--line", "#e6e4de"),
    panel: v("--panel-soft", "#f3f2ee"),
    danger: v("--danger", "#8a352b"),
    palette: PALETTE_FALLBACK.map((fallback, i) => v(`--chart-${i + 1}`, fallback)),
    rampLo: v("--chart-ramp-lo", "#e4ebe7"),
    rampHi: v("--chart-ramp-hi", "#286957"),
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
  const trackTheme = opt._trackTheme === true;
  const slots = seriesSlots(option);
  delete opt._trackTheme;
  delete opt._trackSeriesOrder;
  // Server-generated options carry a default palette; raw ECharts color overrides are intentional.
  if (trackTheme || opt.color === undefined) opt.color = t.palette;
  if (opt.title) {
    opt.title.textStyle = { ...opt.title.textStyle, color: t.text };
  }
  for (const legend of asArray(opt.legend)) {
    legend.textStyle = { ...legend.textStyle, color: t.text };
    legend.itemWidth ??= 32;
    legend.itemHeight ??= 14;
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
    if (!s || typeof s !== "object") return;
    if (s.type === "line") {
      const slot = slots[i];
      const encoding = lineEncoding(s, slot, trackTheme);
      const palette = Array.isArray(opt.color) ? opt.color : [opt.color];
      if (s.color === undefined && palette.length > 0) s.color = palette[slot % palette.length];
      s.lineStyle = { width: 3, ...s.lineStyle, type: encoding.pattern };
      s.symbol = encoding.symbol;
      s.showSymbol = encoding.markers;
      s.showAllSymbol ??= "auto";
      s.symbolSize ??= 7;
    }
    if (trackTheme && s.areaStyle?.color) {
      const color = typeof s.color === "string" ? s.color : t.palette[slots[i] % t.palette.length];
      s.areaStyle.color = s.areaStyle.opacity === undefined ? withAlpha(color, 0.3) : color;
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
      // Roaming pans tiles into the title/visualMap area (ECharts never clips a roamed treemap).
      // Both components already draw above the series (z 6 / z 4 vs 2), so an opaque panel chip —
      // invisible against the page until tiles slide beneath — keeps them legible.
      if (opt.title) {
        opt.title.backgroundColor = t.panel;
      }
      if (opt.visualMap) {
        opt.visualMap.backgroundColor = t.panel;
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

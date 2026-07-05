package render

import (
	"encoding/json"
	"fmt"
	"html"
	"math"
	"strconv"
	"strings"

	"github.com/ttak0422/track/internal/track/viewspec"
)

// echartsCDN is the Apache ECharts bundle the generated page loads. A CDN reference keeps output
// simple and dependency-free; switching to a bundled/pinned asset later is a renderer-local change.
const echartsCDN = "https://cdn.jsdelivr.net/npm/echarts@5"

func init() { Register(ECharts{}) }

// ECharts renders a resolved View Spec as a self-contained HTML page that draws the chart with Apache
// ECharts loaded from a CDN. Unlike the SVG renderer's static output, the page is interactive (hover
// tooltips, legend toggling), and unlike Chart.js — which it replaced — ECharts covers every drawing
// form natively: candlestick, heatmap (visualMap), category-lane scatter (timeline), per-point symbol
// sizes (bubble), and overlay geometry via built-in markLine/markArea.
type ECharts struct{}

// Name identifies this renderer for selection (track render --renderer echarts).
func (ECharts) Name() string { return "echarts" }

// Render builds the ECharts option from the resolved spec and embeds it in a complete HTML document.
func (ECharts) Render(res viewspec.Resolved) (string, error) {
	optJSON, err := EChartsOptionJSON(res)
	if err != nil {
		return "", err
	}
	title := res.Spec.Title
	if title == "" {
		title = "track chart"
	}
	return echartsPage(html.EscapeString(title), optJSON), nil
}

// EChartsOptionJSON builds the ECharts option object for a resolved spec, marshaled to JSON. It is
// the shared core behind the single-chart page (Render), embedded charts in a composed document
// (RenderDocument), and the web workspace's fenced-block endpoint (the frontend hands the option to
// its own ECharts instance, so chart semantics stay decided in Go). Everything emitted is pure JSON —
// per-point symbol sizes instead of size callbacks — so the option survives serialization. Go's json
// escapes <, >, & so the result is safe to inline inside a <script>.
func EChartsOptionJSON(res viewspec.Resolved) (string, error) {
	opt, err := echartsOption(res)
	if err != nil {
		return "", err
	}
	optJSON, err := json.Marshal(opt)
	if err != nil {
		return "", fmt.Errorf("marshal echarts option: %w", err)
	}
	return string(optJSON), nil
}

// echartsOption maps a resolved drawing form onto the ECharts option shape.
func echartsOption(res viewspec.Resolved) (map[string]any, error) {
	opt := map[string]any{
		"color":   seriesPalette,
		"tooltip": map[string]any{"trigger": tooltipTrigger(res.Chart)},
	}
	if res.Spec.Title != "" {
		opt["title"] = map[string]any{"text": res.Spec.Title, "left": "center"}
	}

	switch res.Chart {
	case viewspec.ChartHeatmap:
		buildHeatmap(opt, res)
	case viewspec.ChartTimeline:
		buildTimeline(opt, res)
	case viewspec.ChartBubble:
		buildBubble(opt, res)
	case viewspec.ChartCandlestick:
		buildCandlestick(opt, res)
	case viewspec.ChartHBar:
		buildHBar(opt, res)
	default: // line, area, bar, scatter — category x, numeric y series
		buildSeriesChart(opt, res)
	}

	applyAxisPointer(opt, res.Chart)
	applyDataZoom(opt, res)
	applyOverlays(opt, res)
	return opt, nil
}

// tooltipTrigger picks the hover behavior: category charts read best with the whole axis slice
// (every series at the hovered category), point-shaped charts with the single hovered item.
func tooltipTrigger(t viewspec.ChartType) string {
	switch t {
	case viewspec.ChartBubble, viewspec.ChartTimeline, viewspec.ChartHeatmap, viewspec.ChartScatter:
		return "item"
	}
	return "axis"
}

// applyAxisPointer refines the axis tooltip's hover guide by drawing form: bar-shaped charts
// highlight the hovered category band (shadow), continuous forms get a crosshair with axis value
// readouts. Item-triggered charts keep their plain per-point tooltip.
func applyAxisPointer(opt map[string]any, t viewspec.ChartType) {
	tooltip, ok := opt["tooltip"].(map[string]any)
	if !ok || tooltip["trigger"] != "axis" {
		return
	}
	switch t {
	case viewspec.ChartBar, viewspec.ChartHBar, viewspec.ChartCandlestick:
		tooltip["axisPointer"] = map[string]any{"type": "shadow"}
	default: // line, area
		tooltip["axisPointer"] = map[string]any{"type": "cross"}
	}
}

// dataZoomSliderThreshold is the category count past which a chart gets a visible range slider on
// top of the always-on wheel/pinch zoom: short series don't need one, dense time series (the shape
// the goal articles zoom) do.
// ponytail: fixed count cutoff; derive from label pixel density if charts get configurable widths
const dataZoomSliderThreshold = 30

// applyDataZoom derives zooming mechanically from the drawing form: every category-x chart gets an
// inside (wheel/pinch/drag) zoom on its x axis, and a dense one also gets the slider, so changing
// the visible range needs no spec vocabulary. Value-axis and grid forms (bubble, heatmap) stay
// unzoomed — range selection there reads as noise, not navigation.
func applyDataZoom(opt map[string]any, res viewspec.Resolved) {
	switch res.Chart {
	case viewspec.ChartLine, viewspec.ChartArea, viewspec.ChartBar, viewspec.ChartScatter, viewspec.ChartCandlestick:
	default:
		return
	}
	zooms := []any{map[string]any{"type": "inside", "xAxisIndex": 0}}
	if len(res.Labels) > dataZoomSliderThreshold {
		// The slider stacks above the bottom legend; the grid shrinks so the x labels keep their room.
		zooms = append(zooms, map[string]any{"type": "slider", "xAxisIndex": 0, "bottom": 34, "height": 16})
		opt["grid"] = map[string]any{"bottom": 88}
	}
	opt["dataZoom"] = zooms
}

// buildSeriesChart handles the shared category-x forms: line, area, bar, and scatter.
func buildSeriesChart(opt map[string]any, res viewspec.Resolved) {
	opt["xAxis"] = map[string]any{"type": "category", "data": res.Labels}

	usesY2 := false
	for _, s := range res.Series {
		if s.Axis == "y2" {
			usesY2 = true
		}
	}
	yAxes := []any{map[string]any{"type": "value"}}
	if usesY2 {
		// Keep the secondary axis's gridlines off the chart area so the two scales don't collide.
		yAxes = append(yAxes, map[string]any{"type": "value", "splitLine": map[string]any{"show": false}})
	}
	opt["yAxis"] = yAxes

	var series []any
	var legend []string
	for i, s := range res.Series {
		form := res.SeriesForm(i) // per-series mark overrides compose bars and lines in one chart
		es := map[string]any{
			"name": s.Label,
			"type": echartsSeriesType(form),
			"data": floatsToJSON(s.Values),
		}
		if form == viewspec.ChartArea {
			// Fill down to the baseline at the SVG renderer's 30% opacity so both outputs read alike.
			es["areaStyle"] = map[string]any{"color": seriesFillColor(i), "origin": "start"}
		}
		if res.Stacked && form == viewspec.ChartBar {
			es["stack"] = "total"
		}
		if s.Axis == "y2" {
			es["yAxisIndex"] = 1
		}
		series = append(series, es)
		legend = append(legend, s.Label)
	}
	opt["series"] = series
	if len(legend) > 0 {
		opt["legend"] = map[string]any{"data": legend, "top": "bottom"}
	}
}

// echartsSeriesType maps the category-x drawing forms to ECharts series types; an area is a line with
// an areaStyle (set by the caller).
func echartsSeriesType(t viewspec.ChartType) string {
	switch t {
	case viewspec.ChartArea:
		return "line"
	case viewspec.ChartScatter:
		return "scatter"
	case viewspec.ChartBar:
		return "bar"
	}
	return "line"
}

// buildHBar draws a ranking: categories run down the y-axis (inverse keeps the first — often the
// top-sorted — label at the top, matching the SVG renderer) and the measure runs along x.
func buildHBar(opt map[string]any, res viewspec.Resolved) {
	opt["xAxis"] = map[string]any{"type": "value"}
	opt["yAxis"] = map[string]any{"type": "category", "data": res.Labels, "inverse": true}
	var series []any
	var legend []string
	for _, s := range res.Series {
		es := map[string]any{"name": s.Label, "type": "bar", "data": floatsToJSON(s.Values)}
		if res.Stacked {
			es["stack"] = "total"
		}
		series = append(series, es)
		legend = append(legend, s.Label)
	}
	opt["series"] = series
	if len(legend) > 0 {
		opt["legend"] = map[string]any{"data": legend, "top": "bottom"}
	}
}

// buildBubble plots {x, y, r} points over linear axes, sizing each point individually (per-item
// symbolSize keeps the option pure JSON). A point missing a coordinate is skipped; a missing or
// non-positive radius falls back to a small visible default, like the other renderers.
func buildBubble(opt map[string]any, res viewspec.Resolved) {
	opt["xAxis"] = map[string]any{"type": "value"}
	opt["yAxis"] = map[string]any{"type": "value"}
	var series []any
	var legend []string
	for _, s := range res.Series {
		var data []any
		for _, p := range s.Points {
			if math.IsNaN(p.X) || math.IsNaN(p.Y) {
				continue
			}
			r := p.R
			if math.IsNaN(r) || r <= 0 {
				r = 4
			}
			// symbolSize is a diameter; the resolved radius matches the SVG renderer's pixels.
			data = append(data, map[string]any{"value": []any{p.X, p.Y}, "symbolSize": 2 * r})
		}
		series = append(series, map[string]any{"name": s.Label, "type": "scatter", "data": data})
		legend = append(legend, s.Label)
	}
	opt["series"] = series
	if len(legend) > 0 {
		opt["legend"] = map[string]any{"data": legend, "top": "bottom"}
	}
}

// buildCandlestick draws OHLC candles over the category x-axis. ECharts item order is
// [open, close, lowest, highest]; the resolved series come in CandleSeries (open/high/low/close)
// order. A candle missing any component is a null item (a gap), and the up/down colors match the SVG
// renderer's (green rising, red falling).
func buildCandlestick(opt map[string]any, res viewspec.Resolved) {
	opt["xAxis"] = map[string]any{"type": "category", "data": res.Labels}
	opt["yAxis"] = map[string]any{"type": "value", "scale": true}
	var data []any
	if len(res.Series) == 4 {
		open, high, low, close := res.Series[0].Values, res.Series[1].Values, res.Series[2].Values, res.Series[3].Values
		for i := range res.Labels {
			if i >= len(open) || i >= len(high) || i >= len(low) || i >= len(close) ||
				math.IsNaN(open[i]) || math.IsNaN(high[i]) || math.IsNaN(low[i]) || math.IsNaN(close[i]) {
				data = append(data, nil)
				continue
			}
			data = append(data, []any{open[i], close[i], low[i], high[i]})
		}
	}
	opt["series"] = []any{map[string]any{
		"type": "candlestick",
		"data": data,
		"itemStyle": map[string]any{
			"color": candleUp, "borderColor": candleUp,
			"color0": candleDown, "borderColor0": candleDown,
		},
	}}
}

// buildTimeline plots one dot per grid cell over a category x (time) and category y (lane) axis, the
// swimlane view. Dot sizes scale with the cell value across the same 3–10px radius ramp as the SVG
// renderer; a cell without a value gets the fixed default.
func buildTimeline(opt map[string]any, res viewspec.Resolved) {
	grid := res.Grid
	if grid == nil {
		grid = &viewspec.Grid{}
	}
	opt["xAxis"] = map[string]any{"type": "category", "data": grid.Cols}
	opt["yAxis"] = map[string]any{"type": "category", "data": grid.Rows, "inverse": true}
	lo, hi := gridValueRange(grid.Cells)
	var data []any
	for _, c := range grid.Cells {
		if c.Col >= len(grid.Cols) || c.Row >= len(grid.Rows) {
			continue
		}
		r := 4.0
		item := map[string]any{"value": []any{grid.Cols[c.Col], grid.Rows[c.Row]}}
		if !math.IsNaN(c.Value) {
			r = 3 + 7*(c.Value-lo)/(hi-lo)
			item["value"] = []any{grid.Cols[c.Col], grid.Rows[c.Row], c.Value}
		}
		item["symbolSize"] = 2 * r
		data = append(data, item)
	}
	opt["series"] = []any{map[string]any{"type": "scatter", "data": data}}
}

// buildHeatmap fills the grid cells colored by value through a visualMap using the same light→dark
// blue ramp as the SVG renderer. A cell without a value is simply absent (the background shows).
func buildHeatmap(opt map[string]any, res viewspec.Resolved) {
	grid := res.Grid
	if grid == nil {
		grid = &viewspec.Grid{}
	}
	opt["xAxis"] = map[string]any{"type": "category", "data": grid.Cols}
	opt["yAxis"] = map[string]any{"type": "category", "data": grid.Rows, "inverse": true}
	lo, hi := gridValueRange(grid.Cells)
	var data []any
	for _, c := range grid.Cells {
		if c.Col >= len(grid.Cols) || c.Row >= len(grid.Rows) || math.IsNaN(c.Value) {
			continue
		}
		data = append(data, []any{grid.Cols[c.Col], grid.Rows[c.Row], c.Value})
	}
	opt["visualMap"] = map[string]any{
		"min": lo, "max": hi,
		"calculable": true,
		"orient":     "vertical",
		"right":      0,
		"top":        "center",
		"inRange":    map[string]any{"color": []string{heatColor(0), heatColor(1)}},
	}
	opt["series"] = []any{map[string]any{
		"type":  "heatmap",
		"data":  data,
		"label": map[string]any{"show": false},
	}}
}

// applyOverlays attaches the resolved overlay geometry to the first series: vertical marker lines and
// horizontal reference lines as markLine data, bands as markArea ranges, callouts as markPoint
// bubbles. ECharts scopes mark geometry to a series, so a reference line on the secondary axis rides
// a y2-bound series (any one works; the geometry itself is chart-global). Colors match the other
// renderers' overlay red and band gray.
func applyOverlays(opt map[string]any, res viewspec.Resolved) {
	if len(res.Markers)+len(res.Lines)+len(res.Bands)+len(res.Callouts) == 0 {
		return
	}
	series, ok := opt["series"].([]any)
	if !ok || len(series) == 0 {
		return
	}

	var primary, secondary []any // markLine items per target axis group
	for _, m := range res.Markers {
		item := map[string]any{"xAxis": m.At}
		if m.Label != "" {
			item["label"] = map[string]any{"formatter": m.Label, "position": "insideEndTop"}
		}
		primary = append(primary, item)
	}
	for _, l := range res.Lines {
		item := map[string]any{
			"yAxis":     l.Y,
			"lineStyle": map[string]any{"type": "dashed"},
		}
		if l.Label != "" {
			item["label"] = map[string]any{"formatter": l.Label, "position": "insideEndTop"}
		}
		if l.Axis == "y2" {
			secondary = append(secondary, item)
			continue
		}
		primary = append(primary, item)
	}

	markLine := func(items []any) map[string]any {
		return map[string]any{
			"symbol":    "none",
			"silent":    true,
			"data":      items,
			"lineStyle": map[string]any{"color": "rgba(220,53,69,0.7)"},
			"label":     map[string]any{"color": "rgba(220,53,69,0.9)"},
		}
	}
	first, _ := series[0].(map[string]any)
	if len(primary) > 0 && first != nil {
		first["markLine"] = markLine(primary)
	}
	if len(secondary) > 0 {
		if s := seriesOnY2(series); s != nil {
			s["markLine"] = markLine(secondary)
		}
	}

	if len(res.Bands) > 0 && first != nil {
		var ranges []any
		for _, bd := range res.Bands {
			from := map[string]any{"xAxis": bd.From}
			if bd.Label != "" {
				from["name"] = bd.Label
			}
			ranges = append(ranges, []any{from, map[string]any{"xAxis": bd.To}})
		}
		first["markArea"] = map[string]any{
			"silent":    true,
			"data":      ranges,
			"itemStyle": map[string]any{"color": "rgba(108,117,125,0.15)"},
			"label":     map[string]any{"color": "#6c757d"},
		}
	}

	// Callouts: a small dot on the point plus its text in a bordered bubble above — markPoint with a
	// boxed label reads as a speech bubble without needing free-form drawing primitives.
	if len(res.Callouts) > 0 && first != nil {
		var points []any
		for _, c := range res.Callouts {
			points = append(points, map[string]any{
				"coord": []any{c.X, c.Y},
				"label": map[string]any{"formatter": c.Label},
			})
		}
		first["markPoint"] = map[string]any{
			"silent":     true,
			"symbol":     "circle",
			"symbolSize": 7,
			"itemStyle":  map[string]any{"color": "rgba(220,53,69,0.9)"},
			"data":       points,
			"label": map[string]any{
				"position":        "top",
				"distance":        10,
				"color":           "#333333",
				"backgroundColor": "#ffffff",
				"borderColor":     "rgba(220,53,69,0.9)",
				"borderWidth":     1,
				"borderRadius":    3,
				"padding":         []int{4, 6},
			},
		}
	}
}

// seriesOnY2 finds a series bound to the secondary axis to carry y2 mark geometry; a y2 reference
// line without any y2 series has no scale to sit on and is dropped, like the SVG renderer.
func seriesOnY2(series []any) map[string]any {
	for _, s := range series {
		m, ok := s.(map[string]any)
		if ok && m["yAxisIndex"] == 1 {
			return m
		}
	}
	return nil
}

// seriesFillColor is the shared palette color at the same 30% opacity the SVG renderer fills areas
// with, so an area chart reads identically in HTML and SVG output.
func seriesFillColor(i int) string {
	hex := seriesColor(i) // "#rrggbb" from the shared palette
	v, err := strconv.ParseUint(hex[1:], 16, 32)
	if err != nil {
		return hex
	}
	return fmt.Sprintf("rgba(%d,%d,%d,0.3)", (v>>16)&0xff, (v>>8)&0xff, v&0xff)
}

// floatsToJSON converts series values to a JSON-marshalable slice, mapping NaN (a missing value) to
// null so ECharts renders a gap instead of a false zero. JSON has no NaN, so this also avoids a
// marshal error.
func floatsToJSON(vs []float64) []any {
	out := make([]any, len(vs))
	for i, v := range vs {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			out[i] = nil
			continue
		}
		out[i] = v
	}
	return out
}

// echartsPage wraps a marshaled ECharts option in a minimal, self-contained HTML document.
// escapedTitle must already be HTML-escaped; optionJSON must be valid JSON safe to inline in a
// <script>. The chart fills the viewport and follows window resizes.
func echartsPage(escapedTitle, optionJSON string) string {
	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n")
	b.WriteString(`<html lang="en">` + "\n")
	b.WriteString("<head>\n")
	b.WriteString(`<meta charset="utf-8">` + "\n")
	b.WriteString(`<meta name="viewport" content="width=device-width, initial-scale=1">` + "\n")
	b.WriteString("<title>" + escapedTitle + "</title>\n")
	b.WriteString(`<script src="` + echartsCDN + `"></script>` + "\n")
	b.WriteString("<style>html,body{margin:0;height:100%}#chart{box-sizing:border-box;padding:16px;height:100%}</style>\n")
	b.WriteString("</head>\n")
	b.WriteString("<body>\n")
	b.WriteString(`<div id="chart"></div>` + "\n")
	b.WriteString("<script>\n")
	b.WriteString("const option = " + optionJSON + ";\n")
	b.WriteString(`const chart = echarts.init(document.getElementById("chart"));` + "\n")
	b.WriteString("chart.setOption(option);\n")
	b.WriteString(`addEventListener("resize", () => chart.resize());` + "\n")
	b.WriteString("</script>\n")
	b.WriteString("</body>\n</html>\n")
	return b.String()
}

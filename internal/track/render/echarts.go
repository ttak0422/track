package render

import (
	"encoding/json"
	"fmt"
	"html"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/track/viewspec"
)

// echartsCDN is the Apache ECharts bundle the generated page loads. A CDN reference keeps the
// standalone artifact small (ADR 0029); the version is pinned to the exact ECharts the web frontend
// bundles (web/package.json), so a chart option renders identically on every surface —
// TestEChartsCDNMatchesWebBundle fails when the two drift.
const echartsCDN = "https://cdn.jsdelivr.net/npm/echarts@6.1.0"

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
		opt["title"] = map[string]any{"text": res.Spec.Title, "left": echartsInset}
	}

	switch res.Chart {
	case viewspec.ChartHeatmap:
		buildHeatmap(opt, res)
	case viewspec.ChartTreemap:
		buildTreemap(opt, res)
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
	applyGrid(opt, res.Chart)
	applyDataZoom(opt, res)
	applyOverlays(opt, res)
	return opt, nil
}

// applyGrid stretches the plot across the container so it lines up with the left-aligned title,
// with containLabel keeping the axis labels inside the edges instead of ECharts' default 10%
// side margins. The heatmap keeps right-hand room for its vertical visualMap ramp.
func applyGrid(opt map[string]any, t viewspec.ChartType) {
	if t == viewspec.ChartTreemap {
		return // not a cartesian form: the treemap series carries its own placement
	}
	g := gridOf(opt)
	g["left"] = echartsInset
	g["containLabel"] = true
	if t == viewspec.ChartHeatmap {
		g["right"] = 90
		return
	}
	g["right"] = echartsInset
}

// tooltipTrigger picks the hover behavior: category charts read best with the whole axis slice
// (every series at the hovered category), point-shaped charts with the single hovered item.
func tooltipTrigger(t viewspec.ChartType) string {
	switch t {
	case viewspec.ChartBubble, viewspec.ChartTimeline, viewspec.ChartHeatmap, viewspec.ChartScatter, viewspec.ChartTreemap:
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

// echartsInset is the shared horizontal margin between the container edge and every chart
// element (title, legend, plot, zoom slider), so they align while keeping breathing room.
const echartsInset = 16

// dataZoomSliderThreshold is the category count past which a chart gets a visible range slider on
// top of the inside (Shift+wheel/pinch/drag) zoom: short series don't need one, dense time series
// (the shape the goal articles zoom) do.
// ponytail: fixed count cutoff; derive from label pixel density if charts get configurable widths
const dataZoomSliderThreshold = 30

// applyDataZoom derives zooming mechanically from the drawing form: every category-x chart gets an
// inside (Shift+wheel/pinch/drag) zoom on its x axis, and a dense one also gets the slider, so
// changing the visible range needs no spec vocabulary. Value-axis and grid forms (bubble, heatmap)
// stay unzoomed — range selection there reads as noise, not navigation.
func applyDataZoom(opt map[string]any, res viewspec.Resolved) {
	switch res.Chart {
	case viewspec.ChartLine, viewspec.ChartArea, viewspec.ChartBar, viewspec.ChartScatter, viewspec.ChartCandlestick:
	default:
		return
	}
	zooms := []any{map[string]any{
		"type": "inside", "xAxisIndex": 0,
		// A plain wheel keeps scrolling the page — an ECharts inside-zoom otherwise eats every wheel
		// event over the plot, trapping the reader's scroll mid-note. Zooming asks for Shift+wheel
		// (pinch and the slider still work unmodified).
		"zoomOnMouseWheel": "shift",
	}}
	if len(res.Labels) > dataZoomSliderThreshold {
		// The slider owns the bottom edge (the legend sits up top); the grid shrinks so the x labels
		// keep their room.
		zooms = append(zooms, map[string]any{
			"type": "slider", "xAxisIndex": 0, "bottom": 10, "height": 16,
			"left": echartsInset, "right": echartsInset,
		})
		gridOf(opt)["bottom"] = 64
	}
	opt["dataZoom"] = zooms
}

// applyLegend places the legend between the title and the plot, left-aligned under the title so
// the color→series key reads before the chart; without a title it sits flush with the top edge.
// The plot is pushed down to clear both rows.
func applyLegend(opt map[string]any, labels []string) {
	if len(labels) == 0 {
		return
	}
	top, gridTop := 4, 56
	if _, ok := opt["title"]; ok {
		top, gridTop = 40, 96
	}
	opt["legend"] = map[string]any{"data": labels, "top": top, "left": echartsInset}
	gridOf(opt)["top"] = gridTop
}

// gridOf returns the option's grid map, creating it if needed, so placement tweaks from different
// helpers merge instead of clobbering each other.
func gridOf(opt map[string]any) map[string]any {
	g, ok := opt["grid"].(map[string]any)
	if !ok {
		g = map[string]any{}
		opt["grid"] = g
	}
	return g
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
	// Headroom above the data max keeps overlay callouts and band labels from piling into the
	// legend row at the plot's top edge (static percentages; min stays pinned for bar baselines).
	headroom := []any{0, "12%"}
	yAxes := []any{map[string]any{"type": "value", "boundaryGap": headroom}}
	if usesY2 {
		// Keep the secondary axis's gridlines off the chart area so the two scales don't collide.
		yAxes = append(yAxes, map[string]any{
			"type": "value", "boundaryGap": headroom, "splitLine": map[string]any{"show": false},
		})
	}
	opt["yAxis"] = yAxes

	var series []any
	var legend []string
	for i, s := range res.Series {
		form := res.SeriesForm(i) // per-series mark overrides compose bars and lines in one chart
		es := map[string]any{
			"name": s.Label,
			"type": echartsSeriesType(form),
			"data": seriesData(s),
		}
		if seriesHasHref(s) {
			es["cursor"] = "pointer"
		}
		if form == viewspec.ChartArea {
			// Fill down to the baseline at the SVG renderer's 30% opacity so both outputs read alike.
			es["areaStyle"] = map[string]any{"color": seriesFillColor(i), "origin": "start"}
		}
		if form == viewspec.ChartLine || form == viewspec.ChartArea {
			// No vertex dots, matching the SVG renderer's plain polyline; the axis tooltip still
			// highlights the hovered point.
			es["showSymbol"] = false
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
	applyLegend(opt, legend)
}

// seriesData emits a series' data items: bare numbers normally, {value, ...} objects when the spec's
// provenance channels put extras on the data. ECharts hands an item's fields to event/tooltip params
// untouched, so the frontend reads href/detail from params.data — the option stays pure JSON.
func seriesData(s viewspec.Series) []any {
	vals := floatsToJSON(s.Values)
	if len(s.Extras) == 0 {
		return vals
	}
	out := make([]any, len(vals))
	for i, v := range vals {
		item := map[string]any{"value": v}
		if i < len(s.Extras) {
			ex := s.Extras[i]
			if ex.Href != "" {
				item["href"] = ex.Href
			}
			if ex.Note != "" {
				item["note"] = ex.Note
			}
			if len(ex.Detail) > 0 {
				rows := make([]any, 0, len(ex.Detail))
				for _, kv := range ex.Detail {
					rows = append(rows, map[string]any{"label": kv.Label, "value": kv.Value})
				}
				item["detail"] = rows
			}
		}
		out[i] = item
	}
	return out
}

// seriesHasHref reports whether any datum carries a click target (a source link or a note
// reference), which turns the hover cursor into a pointer so the affordance is visible.
func seriesHasHref(s viewspec.Series) bool {
	for _, ex := range s.Extras {
		if ex.Href != "" || ex.Note != "" {
			return true
		}
	}
	return false
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
		es := map[string]any{"name": s.Label, "type": "bar", "data": seriesData(s)}
		if seriesHasHref(s) {
			es["cursor"] = "pointer"
		}
		if res.Stacked {
			es["stack"] = "total"
		}
		series = append(series, es)
		legend = append(legend, s.Label)
	}
	opt["series"] = series
	applyLegend(opt, legend)
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
	applyLegend(opt, legend)
}

// buildCandlestick draws OHLC candles over the category x-axis. ECharts item order is
// [open, close, lowest, highest]; the resolved series come in CandleSeries (open/high/low/close)
// order. A candle missing any component is a null item (a gap), and the up/down colors match the SVG
// renderer's (green rising, red falling). Resolved series past the four components are the spec's
// explicit y channels — moving-average lines, volume bars — drawn as ordinary series over the candles
// (the candle series holds palette slot 0, so extra j naturally picks palette color j+1, matching the
// SVG renderer's assignment).
func buildCandlestick(opt map[string]any, res viewspec.Resolved) {
	opt["xAxis"] = map[string]any{"type": "category", "data": res.Labels}

	extras := res.Series[min(len(res.Series), len(viewspec.CandleSeries)):]
	yAxes := []any{map[string]any{"type": "value", "scale": true}}
	if usesY2, y2max := candleY2(extras); usesY2 {
		// The secondary axis exists to host volume-style bars under the candles: gridlines and labels
		// stay off (the magnitude is tooltip detail, not an axis to read), and when every y2 series is
		// a bar its max is inflated 4x so the bars hug the bottom band instead of covering the candles.
		axis := map[string]any{
			"type":      "value",
			"splitLine": map[string]any{"show": false},
			"axisLabel": map[string]any{"show": false},
		}
		if y2max > 0 {
			axis["max"] = y2max
		}
		yAxes = append(yAxes, axis)
	}
	opt["yAxis"] = yAxes

	var data []any
	if len(res.Series) >= len(viewspec.CandleSeries) {
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
	series := []any{map[string]any{
		"type": "candlestick",
		"data": data,
		"itemStyle": map[string]any{
			"color": candleUp, "borderColor": candleUp,
			"color0": candleDown, "borderColor0": candleDown,
		},
	}}
	var legend []string
	for _, s := range extras {
		es := map[string]any{
			"name": s.Label,
			"type": echartsSeriesType(s.Mark),
			"data": candleExtraData(s),
		}
		if s.Mark == viewspec.ChartLine || s.Mark == viewspec.ChartArea {
			es["showSymbol"] = false
		}
		if s.Axis == "y2" {
			es["yAxisIndex"] = 1
		}
		series = append(series, es)
		legend = append(legend, s.Label)
	}
	opt["series"] = series
	applyLegend(opt, legend)
}

// candleY2 reports whether any extra series rides the secondary axis, and the axis max to pin when
// the y2 band is volume-shaped (every y2 series a bar): 4x the largest finite value, so the bars
// occupy the bottom quarter under the candles. 0 means leave the max to ECharts.
func candleY2(extras []viewspec.Series) (usesY2 bool, y2max float64) {
	allBars := true
	for _, s := range extras {
		if s.Axis != "y2" {
			continue
		}
		usesY2 = true
		if s.Mark != viewspec.ChartBar {
			allBars = false
		}
		for _, v := range s.Values {
			if !math.IsNaN(v) && !math.IsInf(v, 0) {
				y2max = math.Max(y2max, v)
			}
		}
	}
	if !usesY2 || !allBars {
		return usesY2, 0
	}
	return true, 4 * y2max
}

// candleExtraData emits an extra series' data, coloring each bar datum by its rise hint (green
// rising, red falling — the same colors as the candles) and leaving unknown datums to the series
// color. Line series carry no hints and emit bare values.
func candleExtraData(s viewspec.Series) []any {
	vals := floatsToJSON(s.Values)
	if len(s.Rise) == 0 {
		return vals
	}
	out := make([]any, len(vals))
	for i, v := range vals {
		if i >= len(s.Rise) || s.Rise[i] == 0 {
			out[i] = v
			continue
		}
		c := candleUp
		if s.Rise[i] < 0 {
			c = candleDown
		}
		out[i] = map[string]any{"value": v, "itemStyle": map[string]any{"color": c}}
	}
	return out
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
	opt["visualMap"] = valueVisualMap(res, 0, lo, hi)
	opt["series"] = []any{map[string]any{
		"type":  "heatmap",
		"data":  data,
		"label": map[string]any{"show": false},
	}}
}

// valueVisualMap builds the continuous color legend for a quantitative cell value (heatmap and
// treemap): the sequential light→dark ramp by default, or — with the color channel's scale
// "diverging" — a domain symmetric around zero running the shared market red→neutral→green
// (negative→positive), so treemaps and candlesticks agree on what red and green mean. dimension > 0
// points the map at that index of an array-valued datum (the treemap's [size, value] items).
func valueVisualMap(res viewspec.Resolved, dimension int, lo, hi float64) map[string]any {
	vm := map[string]any{
		"type": "continuous",
		"min":  lo, "max": hi,
		"calculable": true,
		"orient":     "vertical",
		"right":      0,
		"top":        "center",
		"inRange":    map[string]any{"color": []string{heatColor(0), heatColor(1)}},
	}
	if dimension > 0 {
		vm["dimension"] = dimension
	}
	if res.DivergingColor() {
		m := math.Max(math.Abs(lo), math.Abs(hi))
		if m == 0 { // all-zero values: keep a non-degenerate domain
			m = 1
		}
		vm["min"], vm["max"] = -m, m
		vm["inRange"] = map[string]any{"color": []string{candleDown, divergeNeutral, candleUp}}
	}
	return vm
}

// buildTreemap draws area-proportional rectangles (the finviz-style industry map): leaves sized by
// the size channel and colored by the color channel through a dimension-1 visualMap over the items'
// [size, value] pairs. A node with a group (the optional nominal y[0]) nests under a group item —
// groups accumulate in first-seen order, like the grid forms — and grouped maps label the group band
// via upperLabel. A node without a positive size or a finite value is skipped (no area cannot be
// drawn; no value has no color). Breadcrumb and click-zoom are off: the map is a static overview,
// not a drill-down.
func buildTreemap(opt map[string]any, res viewspec.Resolved) {
	tree := res.Tree
	if tree == nil {
		tree = &viewspec.Tree{}
	}
	lo, hi := math.Inf(1), math.Inf(-1)
	var data []any
	groupIdx := map[string]int{}
	grouped := false
	for _, n := range tree.Nodes {
		if math.IsNaN(n.Size) || n.Size <= 0 || math.IsNaN(n.Value) {
			continue
		}
		lo, hi = math.Min(lo, n.Value), math.Max(hi, n.Value)
		item := map[string]any{"name": n.Label, "value": []any{n.Size, n.Value}}
		if n.Group == "" {
			data = append(data, item)
			continue
		}
		grouped = true
		gi, ok := groupIdx[n.Group]
		if !ok {
			gi = len(data)
			groupIdx[n.Group] = gi
			data = append(data, map[string]any{"name": n.Group, "children": []any{}})
		}
		g := data[gi].(map[string]any)
		g["children"] = append(g["children"].([]any), item)
	}
	if math.IsInf(lo, 1) { // no drawable node at all
		lo, hi = 0, 1
	}
	if lo == hi {
		lo, hi = lo-1, hi+1
	}
	opt["visualMap"] = valueVisualMap(res, 1, lo, hi)
	top := 10
	if res.Spec.Title != "" {
		top = 44 // clear the title row, like the cartesian forms' grid top
	}
	series := map[string]any{
		"type": "treemap",
		"data": data,
		// The series places itself (no cartesian grid); the right inset leaves room for the visualMap.
		"left": echartsInset, "right": 90, "top": top, "bottom": 16,
		"breadcrumb": map[string]any{"show": false},
		"nodeClick":  false,
		// Drag-pan / wheel-zoom stays on: a dense map needs zooming to read the small tiles. ECharts
		// never clips a roamed treemap, so panned tiles slide into the title/visualMap area; the web
		// theme layer backs both with an opaque chip (they already draw above the series) so they
		// stay legible.
		"roam":  true,
		"label": map[string]any{"show": true},
		// {c} prints the [size, value] pair, so the tooltip reads "label: size,value".
		"tooltip": map[string]any{"formatter": "{b}: {c}"},
		"levels": []any{
			map[string]any{"itemStyle": map[string]any{"gapWidth": 2, "borderWidth": 2, "borderColor": "#ffffff"}},
			map[string]any{"itemStyle": map[string]any{"gapWidth": 1}},
		},
	}
	if grouped {
		series["upperLabel"] = map[string]any{"show": true, "height": 20}
	}
	opt["series"] = []any{series}
}

// boxDate resolves an annotation box's date line from the marker's x value: an RFC3339 timestamp is
// trimmed to its day, anything else is shown as written. The engine decides the text once so every
// surface renders the same content (no per-consumer date heuristics).
func boxDate(at string) string {
	if t, err := time.Parse(time.RFC3339, at); err == nil {
		return t.Format("2006-01-02")
	}
	return at
}

// boxSource scrubs an annotation box's source link: only http(s) URLs survive, and the display host
// (without a www. prefix) is extracted here so no renderer ever parses a URL — closing the scheme
// hole (javascript: etc.) before any surface sees the link.
func boxSource(href string) (link, host string) {
	if href == "" {
		return "", ""
	}
	u, err := url.Parse(href)
	if err != nil {
		return "", ""
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return href, strings.TrimPrefix(u.Host, "www.")
	}
	return "", ""
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

	// A label along a marker line crosses the series, so it gets a translucent box to stay legible
	// over the data (plain ECharts label options; no free-form drawing needed).
	boxedLabel := func(text string) map[string]any {
		return map[string]any{
			"formatter":       text,
			"position":        "insideEndTop",
			"backgroundColor": "rgba(255,255,255,0.8)",
			"padding":         []any{2, 4},
			"borderRadius":    2,
		}
	}
	var primary, secondary []any // markLine items per target axis group
	linked := false              // any marker carrying provenance makes its markLine clickable
	markerItem := func(m viewspec.Marker) map[string]any {
		item := map[string]any{"xAxis": m.At}
		if m.Label != "" {
			item["label"] = boxedLabel(m.Label)
		}
		if m.Href != "" {
			item["href"] = m.Href
			linked = true
		}
		if m.Note != "" {
			item["note"] = m.Note
			linked = true
		}
		return item
	}
	var boxed []viewspec.Marker
	for _, m := range res.Markers {
		if m.Box {
			boxed = append(boxed, m)
			continue
		}
		primary = append(primary, markerItem(m))
	}
	// Box-mode markers (display: "box", ADR 0028): the item additionally carries an engine-resolved
	// "box" payload — the date line and the source host — and non-http(s) hrefs are scrubbed so no
	// consumer ever parses a URL. Items are emitted sorted by category index (record order breaks
	// ties) so same-day stacks lane deterministically on every surface; a marker whose At matches no
	// label gets no payload (the line is equally unplaceable — skip, never fail). The classic label
	// stays on the item: a bare setOption consumer keeps today's marker look, and the rail-drawing
	// frontend suppresses it on its own clone.
	if len(boxed) > 0 {
		index := make(map[string]int, len(res.Labels))
		for i, l := range res.Labels {
			if _, ok := index[l]; !ok {
				index[l] = i
			}
		}
		at := func(m viewspec.Marker) int {
			if i, ok := index[m.At]; ok {
				return i
			}
			return len(res.Labels) // unmatched markers sort after every placeable one
		}
		sort.SliceStable(boxed, func(a, b int) bool { return at(boxed[a]) < at(boxed[b]) })
		for _, m := range boxed {
			var host string
			m.Href, host = boxSource(m.Href)
			item := markerItem(m)
			if _, ok := index[m.At]; ok {
				box := map[string]any{"date": boxDate(m.At)}
				if host != "" {
					box["host"] = host
				}
				item["box"] = box
			}
			primary = append(primary, item)
		}
	}
	for _, l := range res.Lines {
		item := map[string]any{
			"yAxis":     l.Y,
			"lineStyle": map[string]any{"type": "dashed"},
		}
		if l.Label != "" {
			item["label"] = boxedLabel(l.Label)
		}
		if l.Axis == "y2" {
			secondary = append(secondary, item)
			continue
		}
		primary = append(primary, item)
	}

	markLine := func(items []any) map[string]any {
		return map[string]any{
			"symbol": "none",
			// Silent lines ignore the mouse; provenance-linked markers must stay clickable, so the
			// group wakes up as soon as one marker carries a link.
			"silent":    !linked,
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

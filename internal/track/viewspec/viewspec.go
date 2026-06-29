// Package viewspec defines track's View Spec: a renderer-independent, declarative description of a
// visualization over Canonical Data Model records.
//
// A View Spec names a data source (a JSONL file of one kind), how to map record fields onto chart
// encodings (x and one or more y series), and an optional filter. It deliberately knows nothing about
// Chart.js, SVG, or D3 — a Renderer (see internal/track/render) turns a Spec into concrete output, so
// new renderers can be added without changing the spec. The spec is loaded from a standalone JSON
// file today; the same struct is the unit a future note-embedded (Babel) path would parse, so that
// extension needs no model change.
package viewspec

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/ttak0422/track/internal/track/dataset"
	"github.com/ttak0422/track/internal/track/metric"
)

// Version is the current View Spec schema version. Specs carry it so the format can evolve.
const Version = 1

// ChartType enumerates the visualization kinds. Renderers draw line/bar/hbar/scatter; more (heatmap,
// timeline, narrative) are reserved names that renderers may add later without a spec change.
type ChartType string

const (
	ChartLine     ChartType = "line"
	ChartBar      ChartType = "bar"
	ChartHBar     ChartType = "hbar" // horizontal bar, e.g. a ranking
	ChartScatter  ChartType = "scatter"
	ChartBubble   ChartType = "bubble"   // {x,y,r} points, e.g. sector positions sized by exposure
	ChartHeatmap  ChartType = "heatmap"  // 2D grid: x column × y[0] row, size = cell value (color)
	ChartTimeline ChartType = "timeline" // swimlane dots: x time column × y[0] lane, optional size = radius
)

// RenderableTypes lists the chart types a renderer can draw, in a stable order. It is the single
// source for both validation and help text, so a new chart type shows up in `track render --help`
// automatically.
var RenderableTypes = []ChartType{ChartLine, ChartBar, ChartHBar, ChartScatter, ChartBubble, ChartHeatmap, ChartTimeline}

// gridType reports whether a chart type is a 2D grid (heatmap/timeline) resolved into a Grid rather
// than into x-aligned Series.
func gridType(t ChartType) bool { return t == ChartHeatmap || t == ChartTimeline }

// AxisOptions lists the valid y-series axis assignments (primary/secondary), for help and validation.
var AxisOptions = []string{"y", "y2"}

// renderable reports whether t is a chart type a renderer can draw.
func renderable(t ChartType) bool {
	return slices.Contains(RenderableTypes, t)
}

// Spec is a single visualization.
type Spec struct {
	Version  int        `json:"version"`
	Type     ChartType  `json:"type"`
	Title    string     `json:"title,omitempty"`
	Data     DataRef    `json:"data"`
	Metrics  []Metric   `json:"metrics,omitempty"` // derived per-record fields, computed before encoding
	X        Encoding   `json:"x"`
	Y        []Encoding `json:"y"`
	Size     *Encoding  `json:"size,omitempty"` // bubble radius; required for type bubble
	Filter   *Filter    `json:"filter,omitempty"`
	Overlays []Overlay  `json:"overlays,omitempty"`
}

// Metric is a derived per-record value: offset plus a weighted sum of fields, addressable afterwards
// by Name (so a metric can feed x/y/size/filter, or a later metric). It is a structured linear
// combination — enough for composite indicators like a Pressure Index — deliberately not a free
// expression language.
type Metric struct {
	Name   string  `json:"name"`
	Terms  []Term  `json:"terms"`
	Offset float64 `json:"offset,omitempty"`
}

// Term is one weighted field in a Metric. Weight is a pointer so an omitted weight defaults to 1.0
// (a plain float would make a forgotten weight silently zero out the term, which is a footgun).
type Term struct {
	Field  string   `json:"field"`
	Weight *float64 `json:"weight,omitempty"`
}

// weight returns the term's weight, defaulting an unset weight to 1.0.
func (t Term) weight() float64 {
	if t.Weight == nil {
		return 1
	}
	return *t.Weight
}

// Transform is a series-direction aggregation applied to a resolved y series (e.g. a moving average
// that smooths a noisy index). Window sizes the moving window for sma/ema.
type Transform struct {
	Op     string `json:"op"`
	Window int    `json:"window,omitempty"`
}

// TransformOps lists the valid series-transform operators in a stable order, the single source for
// validation and help text.
var TransformOps = []string{"sma", "ema", "cumsum", "diff"}

// Overlay draws events/annotations from a second data source on top of the chart as vertical markers
// — e.g. plotting policy events along a Pressure Index time series. It reads its own JSONL source
// (typically kind event or annotation): At names the field holding the x position (a value that should
// match an x-axis label), and Label names the field holding the marker text.
type Overlay struct {
	Source string       `json:"source"`
	Kind   dataset.Kind `json:"kind"`
	At     string       `json:"at,omitempty"`    // x-position field; defaults to "time"
	Label  string       `json:"label,omitempty"` // marker text field; defaults to "text"
}

// atField returns the configured x-position field, defaulting to "time".
func (o Overlay) atField() string {
	if o.At != "" {
		return o.At
	}
	return "time"
}

// labelField returns the configured marker-text field, defaulting to "text".
func (o Overlay) labelField() string {
	if o.Label != "" {
		return o.Label
	}
	return "text"
}

// Markers extracts vertical markers from an overlay's records: one per record that has an x position.
// The text is best-effort (empty when the label field is absent), so an event without a title still
// draws its line.
func (o Overlay) Markers(records []dataset.Record) []Marker {
	var ms []Marker
	for _, rec := range records {
		at, ok := rec.String(o.atField())
		if !ok || at == "" {
			continue
		}
		label, _ := rec.String(o.labelField())
		ms = append(ms, Marker{At: at, Label: label})
	}
	return ms
}

// Marker is a resolved vertical overlay: a label drawn at x position At.
type Marker struct {
	At    string
	Label string
}

// DataRef points at the records to plot: a JSONL file (Source, resolved relative to the spec file)
// holding a single canonical Kind.
type DataRef struct {
	Source string       `json:"source"`
	Kind   dataset.Kind `json:"kind"`
}

// Encoding binds a chart axis/series to a record field. Label overrides the legend/axis text, which
// otherwise defaults to the field name. Axis assigns a y series to the primary ("y", default) or
// secondary ("y2") axis, so e.g. a price and an index can share an x-axis on independent scales. Axis
// is ignored for the x encoding.
type Encoding struct {
	Field     string     `json:"field"`
	Label     string     `json:"label,omitempty"`
	Axis      string     `json:"axis,omitempty"`
	Transform *Transform `json:"transform,omitempty"` // series aggregation (y series only, value-chart types)
}

// label returns the user-facing label for an encoding, falling back to the field name.
func (e Encoding) label() string {
	if e.Label != "" {
		return e.Label
	}
	return e.Field
}

// axisID normalizes the target axis, defaulting an empty value to the primary "y".
func (e Encoding) axisID() string {
	if e.Axis == "" {
		return "y"
	}
	return e.Axis
}

// Filter keeps only records matching all of its conditions (logical AND). The shorthand
// {field, equals} expresses a single equality; All carries additional conditions with comparison
// operators, which together cover multi-field, range, and period filtering (e.g. time >= start AND
// time < end). Shorthand and All combine, so {field,equals} plus all[] is a valid mix.
type Filter struct {
	Field  string      `json:"field,omitempty"`  // shorthand: single-field equality
	Equals string      `json:"equals,omitempty"` // shorthand value
	All    []Condition `json:"all,omitempty"`    // every condition must match (AND)
}

// Condition is one field comparison. Op is eq|ne|lt|le|gt|ge (default eq). Ordered comparisons
// (lt/le/gt/ge) compare numerically when both the record value and Value parse as numbers, otherwise
// lexically — so ISO timestamps and version-like strings order correctly without extra typing.
type Condition struct {
	Field string `json:"field"`
	Op    string `json:"op,omitempty"`
	Value string `json:"value"`
}

// FilterOps lists the valid condition operators in a stable order, for validation and help text.
var FilterOps = []string{"eq", "ne", "lt", "le", "gt", "ge"}

// opOrDefault returns the condition's operator, defaulting an empty op to equality.
func (c Condition) opOrDefault() string {
	if c.Op == "" {
		return "eq"
	}
	return c.Op
}

// Load parses a View Spec from JSON and validates it. Unknown fields are rejected so typos in a spec
// surface as errors instead of being silently ignored.
func Load(r io.Reader) (Spec, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	var s Spec
	if err := dec.Decode(&s); err != nil {
		return Spec{}, fmt.Errorf("decode view spec: %w", err)
	}
	if err := s.Validate(); err != nil {
		return Spec{}, err
	}
	return s, nil
}

// Validate checks the spec is renderable: known version, a supported chart type, a data source with a
// known kind, and at least one y series.
func (s Spec) Validate() error {
	if s.Version == 0 {
		return fmt.Errorf("view spec: missing version (current is %d)", Version)
	}
	if s.Version > Version {
		return fmt.Errorf("view spec: version %d is newer than supported %d", s.Version, Version)
	}
	if !renderable(s.Type) {
		return fmt.Errorf("view spec: unsupported type %q (line|bar|scatter)", s.Type)
	}
	if strings.TrimSpace(s.Data.Source) == "" {
		return fmt.Errorf("view spec: data.source is required")
	}
	if !s.Data.Kind.Valid() {
		return fmt.Errorf("view spec: data.kind %q is not a canonical kind", s.Data.Kind)
	}
	if s.X.Field == "" {
		return fmt.Errorf("view spec: x.field is required")
	}
	if len(s.Y) == 0 {
		return fmt.Errorf("view spec: at least one y series is required")
	}
	for i, y := range s.Y {
		if y.Field == "" {
			return fmt.Errorf("view spec: y[%d].field is required", i)
		}
		switch y.Axis {
		case "", "y", "y2":
		default:
			return fmt.Errorf("view spec: y[%d].axis %q is not y or y2", i, y.Axis)
		}
		if y.Transform != nil {
			if err := y.Transform.validate(s.Type, i); err != nil {
				return err
			}
		}
	}
	for i, m := range s.Metrics {
		if strings.TrimSpace(m.Name) == "" {
			return fmt.Errorf("view spec: metrics[%d].name is required", i)
		}
		if len(m.Terms) == 0 {
			return fmt.Errorf("view spec: metrics[%d] (%s) needs at least one term", i, m.Name)
		}
		for j, t := range m.Terms {
			if t.Field == "" {
				return fmt.Errorf("view spec: metrics[%d].terms[%d].field is required", i, j)
			}
		}
	}
	if s.Type == ChartBubble && (s.Size == nil || s.Size.Field == "") {
		return fmt.Errorf("view spec: type bubble requires size.field (the bubble radius)")
	}
	if s.Type == ChartHeatmap && (s.Size == nil || s.Size.Field == "") {
		return fmt.Errorf("view spec: type heatmap requires size.field (the cell value)")
	}
	for i, o := range s.Overlays {
		if strings.TrimSpace(o.Source) == "" {
			return fmt.Errorf("view spec: overlays[%d].source is required", i)
		}
		if !o.Kind.Valid() {
			return fmt.Errorf("view spec: overlays[%d].kind %q is not a canonical kind", i, o.Kind)
		}
	}
	if s.Filter != nil {
		if err := s.Filter.validate(); err != nil {
			return err
		}
	}
	return nil
}

// validate checks the filter has at least one well-formed condition: each names a field and uses a
// known operator. An empty filter object (no shorthand, no all[]) is a likely mistake, so it errors
// rather than silently matching everything.
func (f *Filter) validate() error {
	conds := f.conditions()
	if len(conds) == 0 {
		return fmt.Errorf("view spec: filter has no conditions (set field/equals or all[])")
	}
	for i, c := range conds {
		if c.Field == "" {
			return fmt.Errorf("view spec: filter condition %d is missing field", i)
		}
		if !slices.Contains(FilterOps, c.opOrDefault()) {
			return fmt.Errorf("view spec: filter condition %d op %q is not one of %s", i, c.Op, strings.Join(FilterOps, "|"))
		}
	}
	return nil
}

// conditions flattens the shorthand and All into one AND list. The shorthand contributes an equality
// condition only when its field is set.
func (f *Filter) conditions() []Condition {
	var cs []Condition
	if f.Field != "" {
		cs = append(cs, Condition{Field: f.Field, Op: "eq", Value: f.Equals})
	}
	return append(cs, f.All...)
}

// match reports whether a record passes every condition (logical AND).
func (f *Filter) match(rec dataset.Record) bool {
	for _, c := range f.conditions() {
		if !c.match(rec) {
			return false
		}
	}
	return true
}

// match reports whether a record satisfies one condition.
func (c Condition) match(rec dataset.Record) bool {
	got, _ := rec.String(c.Field)
	switch c.opOrDefault() {
	case "eq":
		return got == c.Value
	case "ne":
		return got != c.Value
	case "lt":
		return compareValues(got, c.Value) < 0
	case "le":
		return compareValues(got, c.Value) <= 0
	case "gt":
		return compareValues(got, c.Value) > 0
	case "ge":
		return compareValues(got, c.Value) >= 0
	}
	return false
}

// compareValues orders two string values numerically when both parse as numbers, else lexically.
func compareValues(a, b string) int {
	af, aerr := strconv.ParseFloat(a, 64)
	bf, berr := strconv.ParseFloat(b, 64)
	if aerr == nil && berr == nil {
		switch {
		case af < bf:
			return -1
		case af > bf:
			return 1
		default:
			return 0
		}
	}
	return strings.Compare(a, b)
}

// Series holds one resolved y series: a label and the value/label pairs already aligned to the shared
// x axis. Missing numeric values are NaN so a renderer can render a gap rather than a false zero.
type Series struct {
	Label  string
	Values []float64
	Axis   string  // "y" (primary) or "y2" (secondary)
	Points []Point // populated instead of Values for bubble charts ({x,y,r} per record)
}

// Point is one bubble datum: position (X, Y) and radius (R). A coordinate is NaN when its field is
// missing, so a renderer can skip an incomplete point rather than plot it at the origin.
type Point struct {
	X, Y, R float64
}

// Grid is the resolved form of a 2D chart (heatmap/timeline): ordered column and row category labels
// plus one Cell per source record. It is populated instead of Series for grid chart types.
type Grid struct {
	Cols  []string // x categories, in first-seen order
	Rows  []string // y[0] categories (rows / lanes), in first-seen order
	Cells []Cell
}

// Cell is one record placed in the grid: Col/Row index into Grid.Cols/Rows, Value is the size
// encoding (heatmap intensity / timeline dot magnitude) or NaN when absent.
type Cell struct {
	Col, Row int
	Value    float64
}

// Resolved is a Spec applied to data: the shared x-axis labels plus one Series per y encoding. A
// Renderer consumes Resolved and never touches raw records, keeping field extraction in one place.
// Grid is set instead of Series for grid chart types (heatmap/timeline).
type Resolved struct {
	Spec    Spec
	Labels  []string
	Series  []Series
	Grid    *Grid
	Markers []Marker // vertical overlays (events/annotations), filled by the caller from Overlays
}

// Resolve applies the spec's filter and encodings to records, producing aligned x labels and y
// series. Records that fail the filter are dropped; a record missing a y value contributes NaN for
// that series so the point becomes a gap.
func (s Spec) Resolve(records []dataset.Record) Resolved {
	res := Resolved{Spec: s}
	s.applyMetrics(records)
	if gridType(s.Type) {
		g := s.resolveGrid(records)
		res.Grid = &g
		return res
	}
	for _, y := range s.Y {
		res.Series = append(res.Series, Series{Label: y.label(), Axis: y.axisID()})
	}
	for _, rec := range records {
		if s.Filter != nil && !s.Filter.match(rec) {
			continue
		}
		if s.Type == ChartBubble {
			s.resolveBubblePoint(rec, &res)
			continue
		}
		x, _ := rec.String(s.X.Field)
		res.Labels = append(res.Labels, x)
		for i, y := range s.Y {
			v, ok := rec.Float(y.Field)
			if !ok {
				v = math.NaN()
			}
			res.Series[i].Values = append(res.Series[i].Values, v)
		}
	}
	for i, y := range s.Y {
		if y.Transform != nil {
			res.Series[i].Values = y.Transform.apply(res.Series[i].Values)
		}
	}
	return res
}

// applyMetrics computes each declared metric into every record under its Name, in declaration order
// so a later metric can reference an earlier one. Records are augmented in place: they are read fresh
// per render, so mutating the maps is cheaper than copying and visible only to this resolve. A metric
// whose any referenced field is missing/non-numeric yields NaN (an incomplete sample → a gap).
func (s Spec) applyMetrics(records []dataset.Record) {
	for _, m := range s.Metrics {
		for _, rec := range records {
			rec[m.Name] = m.compute(rec)
		}
	}
}

// compute evaluates a metric for one record: offset + Σ weight·field. Any missing/non-numeric term
// makes the whole metric NaN, since a composite indicator with a missing component is undefined.
func (m Metric) compute(rec dataset.Record) float64 {
	sum := m.Offset
	for _, t := range m.Terms {
		v, ok := rec.Float(t.Field)
		if !ok {
			return math.NaN()
		}
		sum += t.weight() * v
	}
	return sum
}

// validate checks a y-series transform: a known op, a positive window for the windowed ops, and a
// value-series chart type (transforms operate on a series' Values, which bubble and grid charts do
// not have).
func (t *Transform) validate(typ ChartType, yi int) error {
	if !slices.Contains(TransformOps, t.Op) {
		return fmt.Errorf("view spec: y[%d].transform.op %q is not one of %s", yi, t.Op, strings.Join(TransformOps, "|"))
	}
	if typ == ChartBubble || gridType(typ) {
		return fmt.Errorf("view spec: y[%d].transform is not supported for type %q", yi, typ)
	}
	if (t.Op == "sma" || t.Op == "ema") && t.Window < 1 {
		return fmt.Errorf("view spec: y[%d].transform %q requires window >= 1", yi, t.Op)
	}
	return nil
}

// apply runs the series transform over resolved values, dispatching to the metric engine.
func (t Transform) apply(values []float64) []float64 {
	switch t.Op {
	case "sma":
		return metric.SMA(values, t.Window)
	case "ema":
		return metric.EMA(values, t.Window)
	case "cumsum":
		return metric.CumSum(values)
	case "diff":
		return metric.Diff(values)
	}
	return values
}

// resolveBubblePoint appends one {x,y,r} point per y series for a bubble chart. A missing coordinate
// becomes NaN so the renderer can skip an incomplete point instead of plotting it at the origin.
func (s Spec) resolveBubblePoint(rec dataset.Record, res *Resolved) {
	x, ok := rec.Float(s.X.Field)
	if !ok {
		x = math.NaN()
	}
	r := math.NaN()
	if s.Size != nil {
		if rv, ok := rec.Float(s.Size.Field); ok {
			r = rv
		}
	}
	for i, y := range s.Y {
		yv, ok := rec.Float(y.Field)
		if !ok {
			yv = math.NaN()
		}
		res.Series[i].Points = append(res.Series[i].Points, Point{X: x, Y: yv, R: r})
	}
}

// resolveGrid maps records onto a 2D grid for heatmap/timeline: x.field is the column category,
// y[0].field the row category, and size.field (when set) the cell value. Columns and rows accumulate
// in first-seen order, mirroring the category-axis renderer's input-order labeling. One Cell is
// produced per record; for a heatmap with repeated cells the later record draws on top.
func (s Spec) resolveGrid(records []dataset.Record) Grid {
	var g Grid
	colIdx := map[string]int{}
	rowIdx := map[string]int{}
	intern := func(labels *[]string, idx map[string]int, key string) int {
		if i, ok := idx[key]; ok {
			return i
		}
		i := len(*labels)
		idx[key] = i
		*labels = append(*labels, key)
		return i
	}
	for _, rec := range records {
		if s.Filter != nil && !s.Filter.match(rec) {
			continue
		}
		col, _ := rec.String(s.X.Field)
		row := ""
		if len(s.Y) > 0 {
			row, _ = rec.String(s.Y[0].Field)
		}
		val := math.NaN()
		if s.Size != nil {
			if v, ok := rec.Float(s.Size.Field); ok {
				val = v
			}
		}
		g.Cells = append(g.Cells, Cell{
			Col:   intern(&g.Cols, colIdx, col),
			Row:   intern(&g.Rows, rowIdx, row),
			Value: val,
		})
	}
	return g
}

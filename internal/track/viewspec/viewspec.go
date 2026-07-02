// Package viewspec defines track's View Spec: a renderer-independent, declarative description of a
// visualization over Canonical Data Model records.
//
// A View Spec (schema v2) names a data source (a JSONL file of one kind), a mark (what is drawn), and
// an encoding that maps record fields onto visual channels (x, y series, color, size), plus an optional
// filter. The shape is Vega-Lite-style: mark and encoding are orthogonal, so a channel (color, …) is
// added once and works for every mark, and a mark is added once and gets every channel for free — cost
// grows as marks + channels rather than the old type × feature matrix (see docs/adr/0024). It
// deliberately knows nothing about Chart.js, SVG, or D3 — a Renderer (see internal/track/render) turns a
// Spec into concrete output, so new renderers can be added without changing the spec.
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
)

// Version is the current View Spec schema version. v2 introduced mark + encoding, replacing v1's
// chart type + top-level x/y/size (ADR 0024). Specs carry it so the format can evolve.
const Version = 2

// Mark names what is drawn, orthogonally to how data is encoded onto it. line/bar/point/rect map the
// former chart types; area is a stub that currently draws as a line (fill is a later addition).
type Mark string

const (
	MarkLine  Mark = "line"
	MarkBar   Mark = "bar"   // vertical bars; a nominal y (measure on x) draws them horizontally
	MarkPoint Mark = "point" // scatter/bubble/timeline, distinguished by the encoding's channel types
	MarkArea  Mark = "area"  // stub: drawn as a line until fill lands
	MarkRect  Mark = "rect"  // heatmap: a grid colored by a value channel
)

// Marks lists the marks a spec may use, in a stable order. It is the single source for both validation
// and help text, so a new mark shows up in `track render --help` automatically.
var Marks = []Mark{MarkLine, MarkBar, MarkPoint, MarkArea, MarkRect}

// validMark reports whether m is a drawable mark.
func validMark(m Mark) bool { return slices.Contains(Marks, m) }

// ChannelType classifies a channel's field as a measure (quantitative, the default) or a category
// (nominal). It is the load-bearing hint that lets one mark cover several old chart types: a bar with a
// nominal y is horizontal, a point with a nominal y is a timeline lane, a rect needs both axes nominal.
type ChannelType string

const (
	Quantitative ChannelType = "quantitative"
	Nominal      ChannelType = "nominal"
)

// ChannelTypes lists the valid channel types, for validation and help text.
var ChannelTypes = []ChannelType{Quantitative, Nominal}

// ChartType is the resolved drawing form a renderer draws. It is not part of the spec surface (specs
// name a mark); Resolve computes it from mark + encoding and records it on Resolved so renderers keep a
// single, stable switch over the concrete shape (category-axis series, horizontal bars, linear bubbles,
// or a 2D grid).
type ChartType string

const (
	ChartLine     ChartType = "line"
	ChartBar      ChartType = "bar"
	ChartHBar     ChartType = "hbar"     // horizontal bar (bar mark, nominal y)
	ChartScatter  ChartType = "scatter"  // point mark, category x
	ChartBubble   ChartType = "bubble"   // point mark, quantitative x/y ({x,y,r} on linear axes)
	ChartHeatmap  ChartType = "heatmap"  // rect mark: x × y grid colored by the color channel
	ChartTimeline ChartType = "timeline" // point mark, nominal y: swimlane dots sized by the size channel
)

// AxisOptions lists the valid y-series axis assignments (primary/secondary), for help and validation.
var AxisOptions = []string{"y", "y2"}

// Spec is a single visualization.
type Spec struct {
	Version  int       `json:"version"`
	Mark     Mark      `json:"mark"`
	Title    string    `json:"title,omitempty"`
	Data     DataRef   `json:"data"`
	Encoding Encoding  `json:"encoding"`
	Filter   *Filter   `json:"filter,omitempty"`
	Overlays []Overlay `json:"overlays,omitempty"`
}

// Encoding maps record fields onto the visual channels of a mark. X is the horizontal channel; Y is one
// or more series on the vertical channel(s). Color and Size are optional: a rect (heatmap) reads its
// cell value from Color; a point (bubble/timeline) reads its radius from Size.
type Encoding struct {
	X     Channel   `json:"x"`
	Y     []Channel `json:"y"`
	Color *Channel  `json:"color,omitempty"`
	Size  *Channel  `json:"size,omitempty"`
}

// Channel binds one visual channel to a record field. Title overrides the legend/axis text (defaulting
// to the field name). Type marks the field as quantitative (default) or nominal, which selects the
// drawing form. Axis assigns a y channel to the primary ("y", default) or secondary ("y2") axis; it is
// ignored on non-y channels.
type Channel struct {
	Field string      `json:"field"`
	Title string      `json:"title,omitempty"`
	Type  ChannelType `json:"type,omitempty"`
	Axis  string      `json:"axis,omitempty"`
}

// title returns the user-facing label for a channel, falling back to the field name.
func (c Channel) title() string {
	if c.Title != "" {
		return c.Title
	}
	return c.Field
}

// nominal reports whether the channel's field is a category rather than a measure.
func (c Channel) nominal() bool { return c.Type == Nominal }

// axisID normalizes the target axis, defaulting an empty value to the primary "y".
func (c Channel) axisID() string {
	if c.Axis == "" {
		return "y"
	}
	return c.Axis
}

// chart derives the resolved drawing form from the mark and the encoding's channel types. A bar with a
// nominal y is horizontal; a point is a timeline (nominal y), a bubble (quantitative x), or a category
// scatter (nominal x); a rect is a heatmap; line/area are a line.
func (s Spec) chart() ChartType {
	switch s.Mark {
	case MarkBar:
		if s.yNominal() {
			return ChartHBar
		}
		return ChartBar
	case MarkPoint:
		if s.yNominal() {
			return ChartTimeline
		}
		if s.Encoding.X.nominal() {
			return ChartScatter
		}
		return ChartBubble
	case MarkRect:
		return ChartHeatmap
	default: // line, area
		return ChartLine
	}
}

// yNominal reports whether the first y channel is a category (drives horizontal bars and timeline
// lanes).
func (s Spec) yNominal() bool {
	return len(s.Encoding.Y) > 0 && s.Encoding.Y[0].nominal()
}

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

// DataRef points at the records to plot, as exactly one of: Source (a JSONL file resolved relative to
// the spec file) or Records (data carried inline in the spec). Inline data makes a spec self-contained
// — a single .viewspec.json asset is a complete chart, which the embedded-asset rendering path needs.
// Inline numbers decode as float64 (not json.Number), which Record.Float handles; the standalone CLI
// keeps using Source for external JSONL.
type DataRef struct {
	Source  string           `json:"source,omitempty"`
	Kind    dataset.Kind     `json:"kind"`
	Records []dataset.Record `json:"records,omitempty"`
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

// Validate checks the spec is renderable: a known version, a supported mark, a data source with a known
// kind, a well-formed encoding (an x channel, at least one y channel, valid channel types/axes), and
// the channels a mark needs (rect needs a color value).
func (s Spec) Validate() error {
	if s.Version == 0 {
		return fmt.Errorf("view spec: missing version (current is %d)", Version)
	}
	if s.Version > Version {
		return fmt.Errorf("view spec: version %d is newer than supported %d", s.Version, Version)
	}
	if s.Version < Version {
		return fmt.Errorf("view spec: version %d is no longer supported (current is %d; use mark + encoding)", s.Version, Version)
	}
	if !validMark(s.Mark) {
		return fmt.Errorf("view spec: unsupported mark %q (one of %s)", s.Mark, joinMarks())
	}
	hasSource := strings.TrimSpace(s.Data.Source) != ""
	hasRecords := len(s.Data.Records) > 0
	if !hasSource && !hasRecords {
		return fmt.Errorf("view spec: data needs source (a JSONL file) or records (inline data)")
	}
	if hasSource && hasRecords {
		return fmt.Errorf("view spec: data.source and data.records are mutually exclusive")
	}
	if !s.Data.Kind.Valid() {
		return fmt.Errorf("view spec: data.kind %q is not a canonical kind", s.Data.Kind)
	}
	if err := s.Encoding.validate(); err != nil {
		return err
	}
	if s.Mark == MarkRect && (s.Encoding.Color == nil || s.Encoding.Color.Field == "") {
		return fmt.Errorf("view spec: mark rect requires encoding.color.field (the cell value)")
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

// validate checks the encoding: an x field, at least one y series, and valid types/axes on every
// channel (including the optional color/size).
func (e Encoding) validate() error {
	if e.X.Field == "" {
		return fmt.Errorf("view spec: encoding.x.field is required")
	}
	if err := e.X.validateType("encoding.x"); err != nil {
		return err
	}
	if len(e.Y) == 0 {
		return fmt.Errorf("view spec: at least one encoding.y channel is required")
	}
	for i, y := range e.Y {
		if y.Field == "" {
			return fmt.Errorf("view spec: encoding.y[%d].field is required", i)
		}
		if err := y.validateType(fmt.Sprintf("encoding.y[%d]", i)); err != nil {
			return err
		}
		switch y.Axis {
		case "", "y", "y2":
		default:
			return fmt.Errorf("view spec: encoding.y[%d].axis %q is not y or y2", i, y.Axis)
		}
	}
	for name, ch := range map[string]*Channel{"encoding.color": e.Color, "encoding.size": e.Size} {
		if ch == nil {
			continue
		}
		if ch.Field == "" {
			return fmt.Errorf("view spec: %s.field is required when %s is set", name, name)
		}
		if err := ch.validateType(name); err != nil {
			return err
		}
	}
	return nil
}

// validateType rejects an unknown channel type; an empty type means quantitative.
func (c Channel) validateType(where string) error {
	if c.Type == "" || slices.Contains(ChannelTypes, c.Type) {
		return nil
	}
	return fmt.Errorf("view spec: %s.type %q is not one of %s", where, c.Type, joinChannelTypes())
}

func joinMarks() string {
	ms := make([]string, len(Marks))
	for i, m := range Marks {
		ms[i] = string(m)
	}
	return strings.Join(ms, " | ")
}

func joinChannelTypes() string {
	ts := make([]string, len(ChannelTypes))
	for i, t := range ChannelTypes {
		ts[i] = string(t)
	}
	return strings.Join(ts, " | ")
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

// Cell is one record placed in the grid: Col/Row index into Grid.Cols/Rows, Value is the value
// channel (heatmap color intensity / timeline dot magnitude) or NaN when absent.
type Cell struct {
	Col, Row int
	Value    float64
}

// Resolved is a Spec applied to data: the resolved drawing form plus the shared x-axis labels and one
// Series per y encoding. A Renderer consumes Resolved and never touches raw records, keeping field
// extraction in one place. Grid is set instead of Series for grid forms (heatmap/timeline).
type Resolved struct {
	Spec    Spec
	Chart   ChartType // resolved drawing form (computed from mark + encoding)
	Labels  []string
	Series  []Series
	Grid    *Grid
	Markers []Marker // vertical overlays (events/annotations), filled by the caller from Overlays
}

// Resolve applies the spec's filter and encoding to records, producing the resolved drawing form and
// its aligned data. Records that fail the filter are dropped; a record missing a numeric value
// contributes NaN so the point becomes a gap.
func (s Spec) Resolve(records []dataset.Record) Resolved {
	chart := s.chart()
	res := Resolved{Spec: s, Chart: chart}
	switch chart {
	case ChartHeatmap:
		g := s.resolveGrid(records, s.Encoding.Color)
		res.Grid = &g
	case ChartTimeline:
		g := s.resolveGrid(records, s.Encoding.Size)
		res.Grid = &g
	case ChartBubble:
		s.resolveBubble(records, &res)
	case ChartHBar:
		s.resolveHorizontal(records, &res)
	default: // line, bar, scatter — category x, numeric y series
		s.resolveSeries(records, &res)
	}
	return res
}

// resolveSeries maps records onto category x-axis labels and one numeric y series per y channel — the
// shape line, bar, and scatter share.
func (s Spec) resolveSeries(records []dataset.Record, res *Resolved) {
	for _, y := range s.Encoding.Y {
		res.Series = append(res.Series, Series{Label: y.title(), Axis: y.axisID()})
	}
	for _, rec := range s.filtered(records) {
		x, _ := rec.String(s.Encoding.X.Field)
		res.Labels = append(res.Labels, x)
		for i, y := range s.Encoding.Y {
			res.Series[i].Values = append(res.Series[i].Values, floatOrNaN(rec, y.Field))
		}
	}
}

// resolveHorizontal maps records onto a horizontal bar: the nominal y channel supplies the category
// labels and the quantitative x channel supplies the bar lengths (the axes are swapped relative to a
// vertical bar), matching ADR 0024's "hbar = bar with x/y swapped".
func (s Spec) resolveHorizontal(records []dataset.Record, res *Resolved) {
	res.Series = append(res.Series, Series{Label: s.Encoding.X.title(), Axis: "y"})
	cat := s.Encoding.Y[0]
	for _, rec := range s.filtered(records) {
		label, _ := rec.String(cat.Field)
		res.Labels = append(res.Labels, label)
		res.Series[0].Values = append(res.Series[0].Values, floatOrNaN(rec, s.Encoding.X.Field))
	}
}

// resolveBubble appends one {x,y,r} point per y series for a bubble (point mark on quantitative axes).
// A missing coordinate becomes NaN so the renderer can skip an incomplete point; the radius comes from
// the size channel when set.
func (s Spec) resolveBubble(records []dataset.Record, res *Resolved) {
	for _, y := range s.Encoding.Y {
		res.Series = append(res.Series, Series{Label: y.title(), Axis: "y"})
	}
	for _, rec := range s.filtered(records) {
		x := floatOrNaN(rec, s.Encoding.X.Field)
		r := math.NaN()
		if s.Encoding.Size != nil {
			r = floatOrNaN(rec, s.Encoding.Size.Field)
		}
		for i, y := range s.Encoding.Y {
			res.Series[i].Points = append(res.Series[i].Points, Point{X: x, Y: floatOrNaN(rec, y.Field), R: r})
		}
	}
}

// resolveGrid maps records onto a 2D grid: x.field is the column category, y[0].field the row category,
// and value (the color channel for a heatmap, the size channel for a timeline, when set) the cell value.
// Columns and rows accumulate in first-seen order. One Cell is produced per record; a repeated cell's
// later record draws on top.
func (s Spec) resolveGrid(records []dataset.Record, value *Channel) Grid {
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
	for _, rec := range s.filtered(records) {
		col, _ := rec.String(s.Encoding.X.Field)
		row := ""
		if len(s.Encoding.Y) > 0 {
			row, _ = rec.String(s.Encoding.Y[0].Field)
		}
		val := math.NaN()
		if value != nil {
			val = floatOrNaN(rec, value.Field)
		}
		g.Cells = append(g.Cells, Cell{
			Col:   intern(&g.Cols, colIdx, col),
			Row:   intern(&g.Rows, rowIdx, row),
			Value: val,
		})
	}
	return g
}

// filtered returns the records passing the spec's filter (all of them when there is no filter).
func (s Spec) filtered(records []dataset.Record) []dataset.Record {
	if s.Filter == nil {
		return records
	}
	out := records[:0:0]
	for _, rec := range records {
		if s.Filter.match(rec) {
			out = append(out, rec)
		}
	}
	return out
}

// floatOrNaN reads a numeric field, returning NaN when it is absent so a renderer draws a gap rather
// than a false zero.
func floatOrNaN(rec dataset.Record, field string) float64 {
	if v, ok := rec.Float(field); ok {
		return v
	}
	return math.NaN()
}

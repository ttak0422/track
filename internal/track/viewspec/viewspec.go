// Package viewspec defines track's View Spec: a renderer-independent, declarative description of a
// visualization over Canonical Data Model records.
//
// A View Spec (schema v2) names a data source (a JSONL file of one kind), a mark (what is drawn), and
// an encoding that maps record fields onto visual channels (x, y series, color, size), plus an optional
// filter. The shape is Vega-Lite-style: mark and encoding are orthogonal, so a channel (color, …) is
// added once and works for every mark, and a mark is added once and gets every channel for free — cost
// grows as marks + channels rather than the old type × feature matrix (see docs/adr/0024). It
// deliberately knows nothing about ECharts, SVG, or D3 — a Renderer (see internal/track/render) turns a
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
// former chart types; area is a line with the region down to zero filled; candlestick draws OHLC bars
// from the price kind's canonical fields.
type Mark string

const (
	MarkLine        Mark = "line"
	MarkBar         Mark = "bar"         // vertical bars; a nominal y (measure on x) draws them horizontally
	MarkPoint       Mark = "point"       // scatter/bubble/timeline, distinguished by the encoding's channel types
	MarkArea        Mark = "area"        // a line with the region between it and zero filled
	MarkRect        Mark = "rect"        // heatmap: a grid colored by a value channel
	MarkCandlestick Mark = "candlestick" // OHLC bars for the price kind; open/high/low/close are implied, so no y channels
)

// Marks lists the marks a spec may use, in a stable order. It is the single source for both validation
// and help text, so a new mark shows up in `track render --help` automatically.
var Marks = []Mark{MarkLine, MarkBar, MarkPoint, MarkArea, MarkRect, MarkCandlestick}

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

// SortOptions lists the valid category-axis orderings, for validation and help text.
// ascending/descending order the category labels themselves (numeric-aware, so "9" < "100");
// value/-value order categories by their measure (summed across series when there are several).
var SortOptions = []string{"ascending", "descending", "value", "-value"}

// ChartType is the resolved drawing form a renderer draws. It is not part of the spec surface (specs
// name a mark); Resolve computes it from mark + encoding and records it on Resolved so renderers keep a
// single, stable switch over the concrete shape (category-axis series, horizontal bars, linear bubbles,
// or a 2D grid).
type ChartType string

const (
	ChartLine        ChartType = "line"
	ChartArea        ChartType = "area" // area mark: a line with the region down to zero filled
	ChartBar         ChartType = "bar"
	ChartHBar        ChartType = "hbar"        // horizontal bar (bar mark, nominal y)
	ChartScatter     ChartType = "scatter"     // point mark, category x
	ChartBubble      ChartType = "bubble"      // point mark, quantitative x/y ({x,y,r} on linear axes)
	ChartHeatmap     ChartType = "heatmap"     // rect mark: x × y grid colored by the color channel
	ChartTimeline    ChartType = "timeline"    // point mark, nominal y: swimlane dots sized by the size channel
	ChartCandlestick ChartType = "candlestick" // candlestick mark: OHLC bars over a category x axis
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
// cell value from Color; every other mark reads Color as a nominal category that splits records into
// one series per value (each drawn in its own color); a point (bubble/timeline) reads its radius from
// Size.
type Encoding struct {
	X     Channel   `json:"x"`
	Y     []Channel `json:"y"`
	Color *Channel  `json:"color,omitempty"`
	Size  *Channel  `json:"size,omitempty"`
}

// Channel binds one visual channel to a record field. Title overrides the legend/axis text (defaulting
// to the field name). Type marks the field as quantitative (default) or nominal, which selects the
// drawing form. Axis assigns a y channel to the primary ("y", default) or secondary ("y2") axis; it is
// ignored on non-y channels. Sort and Limit order and truncate the category axis (see SortOptions);
// they are only accepted on the channel that supplies it. Stack stacks a bar's series and is only
// accepted on the bar mark's measure channel. Placement of these options is enforced by Validate, so
// a misplaced option errors instead of being silently ignored.
type Channel struct {
	Field string      `json:"field"`
	Title string      `json:"title,omitempty"`
	Type  ChannelType `json:"type,omitempty"`
	Axis  string      `json:"axis,omitempty"`
	Sort  string      `json:"sort,omitempty"`  // category-axis order: ascending | descending | value | -value
	Limit int         `json:"limit,omitempty"` // keep only the first N categories (after sort): top-N
	Stack bool        `json:"stack,omitempty"` // bar mark: stack the series instead of grouping them
	Mark  Mark        `json:"mark,omitempty"`  // y channels only: draw this series as line|bar|area (a combo chart)
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

// seriesForm maps a y channel's mark override onto its drawing form; empty means the chart default.
func (c Channel) seriesForm() ChartType {
	switch c.Mark {
	case MarkLine:
		return ChartLine
	case MarkBar:
		return ChartBar
	case MarkArea:
		return ChartArea
	}
	return ""
}

// axisID normalizes the target axis, defaulting an empty value to the primary "y".
func (c Channel) axisID() string {
	if c.Axis == "" {
		return "y"
	}
	return c.Axis
}

// chart derives the resolved drawing form from the mark and the encoding's channel types. A bar with a
// nominal y is horizontal; a point is a timeline (nominal y), a bubble (quantitative x), or a category
// scatter (nominal x); a rect is a heatmap; area and candlestick map one-to-one.
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
	case MarkArea:
		return ChartArea
	case MarkCandlestick:
		return ChartCandlestick
	default: // line
		return ChartLine
	}
}

// yNominal reports whether the first y channel is a category (drives horizontal bars and timeline
// lanes).
func (s Spec) yNominal() bool {
	return len(s.Encoding.Y) > 0 && s.Encoding.Y[0].nominal()
}

// labelChannelName names the channel that supplies the category-axis labels for the resolved drawing
// form — where sort/limit apply. It is "" for forms with no category axis (bubble and the grids).
func (s Spec) labelChannelName() string {
	switch s.chart() {
	case ChartLine, ChartArea, ChartBar, ChartScatter, ChartCandlestick:
		return "encoding.x"
	case ChartHBar:
		return "encoding.y[0]"
	}
	return ""
}

// stackChannelName names the measure channel a stack option may sit on: the quantitative axis of a
// bar (y[0] for a vertical bar, x for a horizontal one). It is "" for non-bar forms.
func (s Spec) stackChannelName() string {
	switch s.chart() {
	case ChartBar:
		return "encoding.y[0]"
	case ChartHBar:
		return "encoding.x"
	}
	return ""
}

// labelChannel returns the channel named by labelChannelName (nil when there is none).
func (s Spec) labelChannel() *Channel {
	switch s.labelChannelName() {
	case "encoding.x":
		return &s.Encoding.X
	case "encoding.y[0]":
		return &s.Encoding.Y[0]
	}
	return nil
}

// stacked reports whether the bar's measure channel asks for stacked series.
func (s Spec) stacked() bool {
	switch s.stackChannelName() {
	case "encoding.y[0]":
		return s.Encoding.Y[0].Stack
	case "encoding.x":
		return s.Encoding.X.Stack
	}
	return false
}

// Overlay draws reference geometry on top of the chart. It is a flat union of four shapes,
// discriminated by which fields are set (exactly one per overlay, enforced by Validate):
//
//   - markers: {source|records, kind, at?, label?} — vertical lines read from a second JSONL source
//     or carried inline as records (exactly one of the two; inline keeps an annotated chart
//     self-contained, which a note-embedded spec needs), typically kind event or annotation, e.g.
//     news events along a metric time series. At names the field holding the x position (a value
//     that should match an x-axis label), and Label names the field holding the marker text.
//   - line: {y, axis?, label?} — a horizontal reference line at the literal value Y (a threshold),
//     on the primary ("y", default) or secondary ("y2") axis. Label is the literal line text.
//   - band: {from, to, label?} — a shaded x-range highlighting the period between the From and To
//     category labels (inclusive). Label is the literal band text.
//   - callout: {x, y, label} — a text bubble calling out one data point: the literal Label is drawn
//     in a box near the point at (X category, Y value), connected by a leader. The presence of X
//     (with Y) is what distinguishes a callout from a reference line (Y alone).
type Overlay struct {
	Source  string           `json:"source,omitempty"`
	Records []dataset.Record `json:"records,omitempty"` // marker records carried inline (XOR Source)
	Kind    dataset.Kind     `json:"kind,omitempty"`
	At      string           `json:"at,omitempty"` // x-position field; defaults to "time"

	Y    *float64 `json:"y,omitempty"`    // line: the y value to draw at; callout: the point's value
	Axis string   `json:"axis,omitempty"` // line: "y" (default) or "y2"

	From string `json:"from,omitempty"` // band: first x category (inclusive)
	To   string `json:"to,omitempty"`   // band: last x category (inclusive)

	X string `json:"x,omitempty"` // callout: the point's x category (a value matching an x label)

	// Label is the marker-text field for a source overlay (defaults to "text"), or the literal
	// label text for a line/band/callout overlay.
	Label string `json:"label,omitempty"`
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

// RefLine is a resolved horizontal reference line (a threshold): a value on axis "y" or "y2" plus an
// optional label.
type RefLine struct {
	Y     float64
	Axis  string // "y" (primary) or "y2" (secondary)
	Label string
}

// Band is a resolved x-range highlight: a shaded region spanning the From..To categories (inclusive)
// plus an optional label.
type Band struct {
	From, To string
	Label    string
}

// Callout is a resolved text bubble pointing at one data point: the Label drawn in a box near
// (X category, Y value), connected by a leader.
type Callout struct {
	X     string
	Y     float64
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
	if err := s.Encoding.validate(s.Mark != MarkCandlestick); err != nil {
		return err
	}
	if err := s.validateChannelOptions(); err != nil {
		return err
	}
	if s.Mark == MarkRect && (s.Encoding.Color == nil || s.Encoding.Color.Field == "") {
		return fmt.Errorf("view spec: mark rect requires encoding.color.field (the cell value)")
	}
	// A candlestick reads the price kind's canonical OHLC fields directly, so the vertical encoding is
	// implied: no y channels, and no color/size (the up/down coloring is part of the mark).
	if s.Mark == MarkCandlestick {
		if s.Data.Kind != dataset.KindPrice {
			return fmt.Errorf("view spec: mark candlestick requires data.kind %q (it reads the open/high/low/close fields)", dataset.KindPrice)
		}
		if len(s.Encoding.Y) > 0 {
			return fmt.Errorf("view spec: mark candlestick takes no encoding.y (open/high/low/close are implied by the price kind)")
		}
		if s.Encoding.Color != nil || s.Encoding.Size != nil {
			return fmt.Errorf("view spec: mark candlestick does not take encoding.color or encoding.size")
		}
	}
	// On every mark but rect, color is a nominal category that splits records into one series per
	// value; the constraints keep that split well-defined.
	if s.Encoding.Color != nil && s.Mark != MarkRect {
		if !s.Encoding.Color.nominal() {
			return fmt.Errorf("view spec: encoding.color on mark %s must be type nominal (records split into one series per category)", s.Mark)
		}
		if len(s.Encoding.Y) > 1 {
			return fmt.Errorf("view spec: encoding.color needs a single encoding.y channel (each color category becomes its own series)")
		}
		if s.chart() == ChartTimeline {
			return fmt.Errorf("view spec: encoding.color is not supported on a timeline (lanes are already colored by the nominal y)")
		}
	}
	// Per-series mark overrides compose a combo chart (e.g. bars with a line on y2). They are only
	// well-defined on the vertical series forms, with explicit series (not a color split).
	for i, y := range s.Encoding.Y {
		if y.Mark == "" {
			continue
		}
		switch y.Mark {
		case MarkLine, MarkBar, MarkArea:
		default:
			return fmt.Errorf("view spec: encoding.y[%d].mark %q must be line, bar, or area", i, y.Mark)
		}
		switch s.Mark {
		case MarkLine, MarkBar, MarkArea:
		default:
			return fmt.Errorf("view spec: encoding.y[%d].mark is only supported on line, bar, and area charts", i)
		}
		if s.yNominal() {
			return fmt.Errorf("view spec: encoding.y[%d].mark is not supported on a horizontal bar", i)
		}
		if s.Encoding.Color != nil {
			return fmt.Errorf("view spec: encoding.y[%d].mark cannot combine with encoding.color (split series share one mark)", i)
		}
	}
	for _, nc := range []struct {
		name string
		ch   *Channel
	}{{"encoding.x", &s.Encoding.X}, {"encoding.color", s.Encoding.Color}, {"encoding.size", s.Encoding.Size}} {
		if nc.ch != nil && nc.ch.Mark != "" {
			return fmt.Errorf("view spec: %s.mark is not supported (mark overrides belong on y channels)", nc.name)
		}
	}
	for i, o := range s.Overlays {
		if err := o.validate(i); err != nil {
			return err
		}
	}
	if s.Filter != nil {
		if err := s.Filter.validate(); err != nil {
			return err
		}
	}
	return nil
}

// validate checks the encoding: an x field, at least one y series (unless the mark implies its
// vertical encoding, as candlestick does), and valid types/axes on every channel (including the
// optional color/size).
func (e Encoding) validate(yRequired bool) error {
	if e.X.Field == "" {
		return fmt.Errorf("view spec: encoding.x.field is required")
	}
	if err := e.X.validateType("encoding.x"); err != nil {
		return err
	}
	if yRequired && len(e.Y) == 0 {
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

// validateChannelOptions pins sort/limit/stack to the one channel where each is meaningful for the
// resolved drawing form: sort/limit on the category-axis channel, stack on a bar's measure channel.
// A misplaced option (or one on a form with no such channel) is an error rather than a silent no-op,
// matching the strict-schema stance everywhere else in the spec.
func (s Spec) validateChannelOptions() error {
	type named struct {
		name string
		ch   Channel
	}
	chans := []named{{"encoding.x", s.Encoding.X}}
	for i, y := range s.Encoding.Y {
		chans = append(chans, named{fmt.Sprintf("encoding.y[%d]", i), y})
	}
	if s.Encoding.Color != nil {
		chans = append(chans, named{"encoding.color", *s.Encoding.Color})
	}
	if s.Encoding.Size != nil {
		chans = append(chans, named{"encoding.size", *s.Encoding.Size})
	}
	labelName, stackName := s.labelChannelName(), s.stackChannelName()
	for _, nc := range chans {
		if nc.ch.Sort != "" {
			if !slices.Contains(SortOptions, nc.ch.Sort) {
				return fmt.Errorf("view spec: %s.sort %q is not one of %s", nc.name, nc.ch.Sort, strings.Join(SortOptions, " | "))
			}
			if nc.name != labelName {
				return sortPlacementError(nc.name, "sort", labelName)
			}
		}
		if nc.ch.Limit != 0 {
			if nc.ch.Limit < 0 {
				return fmt.Errorf("view spec: %s.limit must be positive", nc.name)
			}
			if nc.name != labelName {
				return sortPlacementError(nc.name, "limit", labelName)
			}
		}
		if nc.ch.Stack && nc.name != stackName {
			if stackName == "" {
				return fmt.Errorf("view spec: %s.stack applies only to mark bar", nc.name)
			}
			return fmt.Errorf("view spec: %s.stack belongs on the bar's measure channel (%s)", nc.name, stackName)
		}
	}
	return nil
}

// sortPlacementError explains where a misplaced sort/limit belongs, or that the drawing form has no
// category axis at all.
func sortPlacementError(where, opt, labelName string) error {
	if labelName == "" {
		return fmt.Errorf("view spec: %s.%s needs a category axis (this mark/encoding has none)", where, opt)
	}
	return fmt.Errorf("view spec: %s.%s belongs on the category-axis channel (%s)", where, opt, labelName)
}

// validate checks an overlay is exactly one of its four shapes (markers / line / band / callout) and
// that the chosen shape is complete: a marker overlay needs a canonical kind and exactly one of
// source or records, a line's axis must be y or y2, a band needs both ends, and a callout needs its
// point (x, y) and text. Fields of another shape are rejected rather than ignored, so a mixed overlay
// surfaces as an error.
func (o Overlay) validate(i int) error {
	hasSource := strings.TrimSpace(o.Source) != ""
	hasRecords := len(o.Records) > 0
	hasCallout := o.X != ""
	hasLine := o.Y != nil && !hasCallout
	hasBand := o.From != "" || o.To != ""
	shapes := 0
	for _, set := range []bool{hasSource || hasRecords, hasLine, hasBand, hasCallout} {
		if set {
			shapes++
		}
	}
	if shapes != 1 {
		return fmt.Errorf("view spec: overlays[%d] must be exactly one of markers {source|records, kind}, line {y}, band {from, to}, or callout {x, y, label}", i)
	}
	switch {
	case hasSource || hasRecords:
		if hasSource && hasRecords {
			return fmt.Errorf("view spec: overlays[%d] takes exactly one of source or records", i)
		}
		if !o.Kind.Valid() {
			return fmt.Errorf("view spec: overlays[%d].kind %q is not a canonical kind", i, o.Kind)
		}
		if o.Axis != "" {
			return fmt.Errorf("view spec: overlays[%d].axis applies only to a line overlay", i)
		}
		if err := dataset.ValidateRecords(o.Kind, o.Records); err != nil {
			return fmt.Errorf("view spec: overlays[%d] records: %w", i, err)
		}
	case hasLine:
		if o.Kind != "" || o.At != "" {
			return fmt.Errorf("view spec: overlays[%d] line overlay does not take kind/at", i)
		}
		if o.Axis != "" && !slices.Contains(AxisOptions, o.Axis) {
			return fmt.Errorf("view spec: overlays[%d].axis %q is not y or y2", i, o.Axis)
		}
	case hasCallout:
		if o.Y == nil {
			return fmt.Errorf("view spec: overlays[%d] callout needs both x and y (the point to call out)", i)
		}
		if strings.TrimSpace(o.Label) == "" {
			return fmt.Errorf("view spec: overlays[%d] callout needs a label (the bubble text)", i)
		}
		if o.Kind != "" || o.At != "" || o.Axis != "" {
			return fmt.Errorf("view spec: overlays[%d] callout overlay does not take kind/at/axis", i)
		}
	default: // band
		if o.From == "" || o.To == "" {
			return fmt.Errorf("view spec: overlays[%d] band needs both from and to", i)
		}
		if o.Kind != "" || o.At != "" || o.Axis != "" {
			return fmt.Errorf("view spec: overlays[%d] band overlay does not take kind/at/axis", i)
		}
	}
	return nil
}

// axisID normalizes a line overlay's target axis, defaulting an empty value to the primary "y".
func (o Overlay) axisID() string {
	if o.Axis == "" {
		return "y"
	}
	return o.Axis
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
	Axis   string    // "y" (primary) or "y2" (secondary)
	Points []Point   // populated instead of Values for bubble charts ({x,y,r} per record)
	Mark   ChartType // per-series drawing form override (combo charts); empty = the chart's form
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
	Spec     Spec
	Chart    ChartType // resolved drawing form (computed from mark + encoding)
	Stacked  bool      // bar forms: draw the series stacked instead of grouped side by side
	Labels   []string
	Series   []Series
	Grid     *Grid
	Markers  []Marker  // vertical overlays (events/annotations): inline records fill them in Resolve, source overlays are filled by the caller from Overlays
	Lines    []RefLine // horizontal reference lines, filled by Resolve (they carry no data source)
	Bands    []Band    // x-range highlights, filled by Resolve (they carry no data source)
	Callouts []Callout // text bubbles pointing at data points, filled by Resolve (literal values)
}

// SeriesForm is the drawing form of one series: its mark override when set (a combo chart), else the
// chart's overall form. Renderers draw each series by this, so bars and lines mix in one plot.
func (r Resolved) SeriesForm(i int) ChartType {
	if i < len(r.Series) && r.Series[i].Mark != "" {
		return r.Series[i].Mark
	}
	return r.Chart
}

// Resolve applies the spec's filter and encoding to records, producing the resolved drawing form and
// its aligned data. Records that fail the filter are dropped; a record missing a numeric value
// contributes NaN so the point becomes a gap.
func (s Spec) Resolve(records []dataset.Record) Resolved {
	chart := s.chart()
	res := Resolved{Spec: s, Chart: chart, Stacked: s.stacked()}
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
	case ChartCandlestick:
		s.resolveCandles(records, &res)
	default: // line, area, bar, scatter — category x, numeric y series
		s.resolveSeries(records, &res)
	}
	// Sort/limit reorder and truncate the category axis after the series are aligned, so they compose
	// with everything above (multi-series, a color split, horizontal bars) in one place.
	if ch := s.labelChannel(); ch != nil {
		sortAndLimit(&res, *ch)
	}
	// Overlays that carry no second data source (line/band/callout literals, inline marker records)
	// resolve here; source overlays need file IO and stay with the caller (Resolved.Markers).
	for _, o := range s.Overlays {
		switch {
		case o.X != "" && o.Y != nil:
			res.Callouts = append(res.Callouts, Callout{X: o.X, Y: *o.Y, Label: o.Label})
		case o.Y != nil:
			res.Lines = append(res.Lines, RefLine{Y: *o.Y, Axis: o.axisID(), Label: o.Label})
		case o.From != "":
			res.Bands = append(res.Bands, Band{From: o.From, To: o.To, Label: o.Label})
		case len(o.Records) > 0:
			res.Markers = append(res.Markers, o.Markers(o.Records)...)
		}
	}
	return res
}

// resolveSeries maps records onto category x-axis labels and one numeric y series per y channel — the
// shape line, bar, and scatter share.
func (s Spec) resolveSeries(records []dataset.Record, res *Resolved) {
	if s.Encoding.Color != nil {
		s.resolveColorSeries(records, res, s.Encoding.X.Field, s.Encoding.Y[0].Field, s.Encoding.Y[0].axisID())
		return
	}
	for _, y := range s.Encoding.Y {
		res.Series = append(res.Series, Series{Label: y.title(), Axis: y.axisID(), Mark: y.seriesForm()})
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
	if s.Encoding.Color != nil {
		s.resolveColorSeries(records, res, s.Encoding.Y[0].Field, s.Encoding.X.Field, "y")
		return
	}
	res.Series = append(res.Series, Series{Label: s.Encoding.X.title(), Axis: "y"})
	cat := s.Encoding.Y[0]
	for _, rec := range s.filtered(records) {
		label, _ := rec.String(cat.Field)
		res.Labels = append(res.Labels, label)
		res.Series[0].Values = append(res.Series[0].Values, floatOrNaN(rec, s.Encoding.X.Field))
	}
}

// resolveColorSeries splits records into one series per color category: labelField supplies the shared
// category axis (x for the vertical forms, the nominal y for horizontal bars), valueField the measure.
// Labels and series accumulate in first-seen order (which keeps output deterministic for a given
// input), several records may share one label slot (one per category), and every series is aligned to
// the shared axis with NaN gaps so a category missing at a label renders as a gap.
func (s Spec) resolveColorSeries(records []dataset.Record, res *Resolved, labelField, valueField, axis string) {
	labelIdx := map[string]int{}
	seriesIdx := map[string]int{}
	for _, rec := range s.filtered(records) {
		label, _ := rec.String(labelField)
		li, ok := labelIdx[label]
		if !ok {
			li = len(res.Labels)
			labelIdx[label] = li
			res.Labels = append(res.Labels, label)
		}
		cat, _ := rec.String(s.Encoding.Color.Field)
		si, ok := seriesIdx[cat]
		if !ok {
			si = len(res.Series)
			seriesIdx[cat] = si
			res.Series = append(res.Series, Series{Label: cat, Axis: axis})
		}
		vals := &res.Series[si].Values
		for len(*vals) < li {
			*vals = append(*vals, math.NaN())
		}
		v := floatOrNaN(rec, valueField)
		if len(*vals) == li {
			*vals = append(*vals, v)
		} else {
			(*vals)[li] = v // repeated (label, category): the later record wins, like grid cells
		}
	}
	for i := range res.Series {
		for len(res.Series[i].Values) < len(res.Labels) {
			res.Series[i].Values = append(res.Series[i].Values, math.NaN())
		}
	}
}

// CandleSeries is the fixed order of the four aligned series a candlestick resolves to — the contract
// a renderer indexes by. The fields are the price kind's canonical OHLC columns.
var CandleSeries = []string{"open", "high", "low", "close"}

// resolveCandles maps price records onto a candlestick: the x channel supplies the category labels and
// the four canonical OHLC fields become four series aligned to them, in CandleSeries order. A record
// missing a component contributes NaN, so the renderer skips that candle rather than drawing a wrong
// one.
func (s Spec) resolveCandles(records []dataset.Record, res *Resolved) {
	for _, f := range CandleSeries {
		res.Series = append(res.Series, Series{Label: f, Axis: "y"})
	}
	for _, rec := range s.filtered(records) {
		x, _ := rec.String(s.Encoding.X.Field)
		res.Labels = append(res.Labels, x)
		for i, f := range CandleSeries {
			res.Series[i].Values = append(res.Series[i].Values, floatOrNaN(rec, f))
		}
	}
}

// resolveBubble appends one {x,y,r} point per y series for a bubble (point mark on quantitative axes).
// A missing coordinate becomes NaN so the renderer can skip an incomplete point; the radius comes from
// the size channel when set. A color channel splits the points into one series per category instead
// (first-seen order), so each category plots in its own color.
func (s Spec) resolveBubble(records []dataset.Record, res *Resolved) {
	if s.Encoding.Color != nil {
		seriesIdx := map[string]int{}
		for _, rec := range s.filtered(records) {
			cat, _ := rec.String(s.Encoding.Color.Field)
			si, ok := seriesIdx[cat]
			if !ok {
				si = len(res.Series)
				seriesIdx[cat] = si
				res.Series = append(res.Series, Series{Label: cat, Axis: "y"})
			}
			res.Series[si].Points = append(res.Series[si].Points, s.bubblePoint(rec, s.Encoding.Y[0]))
		}
		return
	}
	for _, y := range s.Encoding.Y {
		res.Series = append(res.Series, Series{Label: y.title(), Axis: "y"})
	}
	for _, rec := range s.filtered(records) {
		for i, y := range s.Encoding.Y {
			res.Series[i].Points = append(res.Series[i].Points, s.bubblePoint(rec, y))
		}
	}
}

// bubblePoint reads one {x,y,r} point from a record: x from the x channel, y from the given y channel,
// and the radius from the size channel when set (NaN otherwise, which renderers default).
func (s Spec) bubblePoint(rec dataset.Record, y Channel) Point {
	r := math.NaN()
	if s.Encoding.Size != nil {
		r = floatOrNaN(rec, s.Encoding.Size.Field)
	}
	return Point{X: floatOrNaN(rec, s.Encoding.X.Field), Y: floatOrNaN(rec, y.Field), R: r}
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

// sortAndLimit reorders the resolved category axis per the label channel's sort option and truncates
// it to the channel's limit (top-N). Labels and every series' values move together, so the alignment
// established by the resolvers is preserved. Sorting is stable, so ties keep first-seen order and the
// output stays deterministic.
func sortAndLimit(res *Resolved, ch Channel) {
	if ch.Sort != "" && len(res.Labels) > 1 {
		idx := make([]int, len(res.Labels))
		for i := range idx {
			idx[i] = i
		}
		var cmp func(a, b int) int
		switch ch.Sort {
		case "ascending":
			cmp = func(a, b int) int { return compareValues(res.Labels[a], res.Labels[b]) }
		case "descending":
			cmp = func(a, b int) int { return compareValues(res.Labels[b], res.Labels[a]) }
		case "value":
			cmp = func(a, b int) int { return compareFloats(labelValue(res, a), labelValue(res, b)) }
		default: // "-value"
			cmp = func(a, b int) int { return compareFloats(labelValue(res, b), labelValue(res, a)) }
		}
		slices.SortStableFunc(idx, cmp)
		res.Labels = permuteStrings(res.Labels, idx)
		for i := range res.Series {
			res.Series[i].Values = permuteFloats(res.Series[i].Values, idx)
		}
	}
	if ch.Limit > 0 && len(res.Labels) > ch.Limit {
		res.Labels = res.Labels[:ch.Limit]
		for i := range res.Series {
			res.Series[i].Values = res.Series[i].Values[:ch.Limit]
		}
	}
}

// labelValue is the measure a value sort orders a category by: its finite series values summed (so a
// multi-series or stacked chart sorts by the total). A category with no finite value at all sums to 0.
func labelValue(res *Resolved, i int) float64 {
	sum := 0.0
	for _, s := range res.Series {
		if i < len(s.Values) && !math.IsNaN(s.Values[i]) && !math.IsInf(s.Values[i], 0) {
			sum += s.Values[i]
		}
	}
	return sum
}

// compareFloats orders two float64s for sorting.
func compareFloats(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// permuteStrings returns ss reordered so element i comes from ss[idx[i]].
func permuteStrings(ss []string, idx []int) []string {
	out := make([]string, len(idx))
	for i, j := range idx {
		out[i] = ss[j]
	}
	return out
}

// permuteFloats returns vs reordered so element i comes from vs[idx[i]]; a short series (defensive —
// resolvers always align) contributes NaN.
func permuteFloats(vs []float64, idx []int) []float64 {
	out := make([]float64, len(idx))
	for i, j := range idx {
		if j < len(vs) {
			out[i] = vs[j]
		} else {
			out[i] = math.NaN()
		}
	}
	return out
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

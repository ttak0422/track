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
	"strings"

	"github.com/ttak0422/track/internal/track/dataset"
)

// Version is the current View Spec schema version. Specs carry it so the format can evolve.
const Version = 1

// ChartType enumerates the visualization kinds. MVP renders line/bar/scatter; more (heatmap,
// timeline, narrative) are reserved names that renderers may add later without a spec change.
type ChartType string

const (
	ChartLine    ChartType = "line"
	ChartBar     ChartType = "bar"
	ChartScatter ChartType = "scatter"
)

// renderableTypes are the chart types the MVP renderer supports.
var renderableTypes = map[ChartType]bool{ChartLine: true, ChartBar: true, ChartScatter: true}

// Spec is a single visualization.
type Spec struct {
	Version int        `json:"version"`
	Type    ChartType  `json:"type"`
	Title   string     `json:"title,omitempty"`
	Data    DataRef    `json:"data"`
	X       Encoding   `json:"x"`
	Y       []Encoding `json:"y"`
	Filter  *Filter    `json:"filter,omitempty"`
}

// DataRef points at the records to plot: a JSONL file (Source, resolved relative to the spec file)
// holding a single canonical Kind.
type DataRef struct {
	Source string       `json:"source"`
	Kind   dataset.Kind `json:"kind"`
}

// Encoding binds a chart axis/series to a record field. Label overrides the legend/axis text, which
// otherwise defaults to the field name.
type Encoding struct {
	Field string `json:"field"`
	Label string `json:"label,omitempty"`
}

// label returns the user-facing label for an encoding, falling back to the field name.
func (e Encoding) label() string {
	if e.Label != "" {
		return e.Label
	}
	return e.Field
}

// Filter keeps only records whose Field equals Equals. It is intentionally minimal (single equality);
// richer querying belongs in a future query layer, not the spec.
type Filter struct {
	Field  string `json:"field"`
	Equals string `json:"equals"`
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
	if !renderableTypes[s.Type] {
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
	}
	return nil
}

// Series holds one resolved y series: a label and the value/label pairs already aligned to the shared
// x axis. Missing numeric values are NaN so a renderer can render a gap rather than a false zero.
type Series struct {
	Label  string
	Values []float64
}

// Resolved is a Spec applied to data: the shared x-axis labels plus one Series per y encoding. A
// Renderer consumes Resolved and never touches raw records, keeping field extraction in one place.
type Resolved struct {
	Spec   Spec
	Labels []string
	Series []Series
}

// Resolve applies the spec's filter and encodings to records, producing aligned x labels and y
// series. Records that fail the filter are dropped; a record missing a y value contributes NaN for
// that series so the point becomes a gap.
func (s Spec) Resolve(records []dataset.Record) Resolved {
	res := Resolved{Spec: s}
	for _, y := range s.Y {
		res.Series = append(res.Series, Series{Label: y.label()})
	}
	for _, rec := range records {
		if s.Filter != nil {
			got, _ := rec.String(s.Filter.Field)
			if got != s.Filter.Equals {
				continue
			}
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
	return res
}

package viewspec

import (
	"math"
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/track/dataset"
)

const goodSpec = `{
  "version": 1,
  "type": "line",
  "title": "AAPL",
  "data": {"source": "prices.jsonl", "kind": "price"},
  "x": {"field": "time"},
  "y": [{"field": "close", "label": "Close"}],
  "filter": {"field": "entity", "equals": "AAPL"}
}`

func TestLoadValid(t *testing.T) {
	s, err := Load(strings.NewReader(goodSpec))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.Type != ChartLine || s.Data.Kind != dataset.KindPrice || len(s.Y) != 1 {
		t.Fatalf("unexpected spec: %+v", s)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	_, err := Load(strings.NewReader(`{"version":1,"type":"line","data":{"source":"x","kind":"price"},"x":{"field":"t"},"y":[{"field":"c"}],"bogus":1}`))
	if err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("want unknown-field error, got %v", err)
	}
}

func TestValidateErrors(t *testing.T) {
	cases := map[string]Spec{
		"missing version": {Type: ChartLine, Data: DataRef{Source: "x", Kind: dataset.KindPrice}, X: Encoding{Field: "t"}, Y: []Encoding{{Field: "c"}}},
		"bad type":        {Version: 1, Type: "pie", Data: DataRef{Source: "x", Kind: dataset.KindPrice}, X: Encoding{Field: "t"}, Y: []Encoding{{Field: "c"}}},
		"bad kind":        {Version: 1, Type: ChartLine, Data: DataRef{Source: "x", Kind: "bogus"}, X: Encoding{Field: "t"}, Y: []Encoding{{Field: "c"}}},
		"no source":       {Version: 1, Type: ChartLine, Data: DataRef{Kind: dataset.KindPrice}, X: Encoding{Field: "t"}, Y: []Encoding{{Field: "c"}}},
		"no x":            {Version: 1, Type: ChartLine, Data: DataRef{Source: "x", Kind: dataset.KindPrice}, Y: []Encoding{{Field: "c"}}},
		"no y":            {Version: 1, Type: ChartLine, Data: DataRef{Source: "x", Kind: dataset.KindPrice}, X: Encoding{Field: "t"}},
		"future version":  {Version: Version + 1, Type: ChartLine, Data: DataRef{Source: "x", Kind: dataset.KindPrice}, X: Encoding{Field: "t"}, Y: []Encoding{{Field: "c"}}},
	}
	for name, s := range cases {
		if err := s.Validate(); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestResolveFiltersAndAligns(t *testing.T) {
	s, _ := Load(strings.NewReader(goodSpec))
	recs, _ := dataset.ReadJSONL(strings.NewReader(
		`{"time":"d1","entity":"AAPL","close":10}` + "\n" +
			`{"time":"d2","entity":"MSFT","close":99}` + "\n" +
			`{"time":"d3","entity":"AAPL","close":12}` + "\n"))
	res := s.Resolve(recs)
	if want := []string{"d1", "d3"}; !equalStrings(res.Labels, want) {
		t.Fatalf("labels = %v, want %v", res.Labels, want)
	}
	if len(res.Series) != 1 || res.Series[0].Label != "Close" {
		t.Fatalf("series = %+v", res.Series)
	}
	if got := res.Series[0].Values; got[0] != 10 || got[1] != 12 {
		t.Fatalf("values = %v", got)
	}
}

func TestFilterAllConditionsAND(t *testing.T) {
	// entity == AAPL AND time in [d2, d4): keeps d2 and d3, drops d1 (entity) and d4 (range).
	f := &Filter{All: []Condition{
		{Field: "entity", Value: "AAPL"},
		{Field: "time", Op: "ge", Value: "d2"},
		{Field: "time", Op: "lt", Value: "d4"},
	}}
	if err := f.validate(); err != nil {
		t.Fatal(err)
	}
	recs, _ := dataset.ReadJSONL(strings.NewReader(
		`{"time":"d1","entity":"AAPL"}` + "\n" +
			`{"time":"d2","entity":"AAPL"}` + "\n" +
			`{"time":"d3","entity":"MSFT"}` + "\n" +
			`{"time":"d4","entity":"AAPL"}` + "\n"))
	var kept []string
	for _, r := range recs {
		if f.match(r) {
			t, _ := r.String("time")
			kept = append(kept, t)
		}
	}
	if want := []string{"d2"}; !equalStrings(kept, want) {
		t.Fatalf("kept = %v, want %v", kept, want)
	}
}

func TestFilterNumericRange(t *testing.T) {
	// gt compares numerically: "9" < "100" even though lexically "9" > "100".
	f := &Filter{All: []Condition{{Field: "v", Op: "gt", Value: "9"}}}
	recs, _ := dataset.ReadJSONL(strings.NewReader(
		`{"v":5}` + "\n" + `{"v":100}` + "\n"))
	if f.match(recs[0]) {
		t.Fatal("v=5 should be excluded by v>9")
	}
	if !f.match(recs[1]) {
		t.Fatal("v=100 should be included by v>9 (numeric, not lexical)")
	}
}

func TestFilterValidateRejectsBadOpAndEmpty(t *testing.T) {
	if err := (&Filter{All: []Condition{{Field: "x", Op: "like", Value: "y"}}}).validate(); err == nil {
		t.Fatal("expected error for unknown op")
	}
	if err := (&Filter{}).validate(); err == nil {
		t.Fatal("expected error for empty filter")
	}
}

func TestResolveGridHeatmap(t *testing.T) {
	s := Spec{
		Version: 1, Type: ChartHeatmap,
		Data: DataRef{Source: "x", Kind: dataset.KindMetric},
		X:    Encoding{Field: "col"}, Y: []Encoding{{Field: "row"}}, Size: &Encoding{Field: "v"},
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	recs, _ := dataset.ReadJSONL(strings.NewReader(
		`{"col":"Q1","row":"Tech","v":1}` + "\n" +
			`{"col":"Q2","row":"Tech","v":9}` + "\n" +
			`{"col":"Q1","row":"Energy"}` + "\n")) // missing value → NaN cell
	res := s.Resolve(recs)
	if res.Grid == nil {
		t.Fatal("grid type should populate Grid")
	}
	if !equalStrings(res.Grid.Cols, []string{"Q1", "Q2"}) || !equalStrings(res.Grid.Rows, []string{"Tech", "Energy"}) {
		t.Fatalf("cols/rows = %v / %v", res.Grid.Cols, res.Grid.Rows)
	}
	if len(res.Grid.Cells) != 3 || res.Grid.Cells[1].Value != 9 {
		t.Fatalf("cells = %+v", res.Grid.Cells)
	}
	if !math.IsNaN(res.Grid.Cells[2].Value) {
		t.Fatal("missing size should be NaN cell")
	}
}

func TestHeatmapRequiresSize(t *testing.T) {
	s := Spec{Version: 1, Type: ChartHeatmap, Data: DataRef{Source: "x", Kind: dataset.KindMetric}, X: Encoding{Field: "c"}, Y: []Encoding{{Field: "r"}}}
	if err := s.Validate(); err == nil {
		t.Fatal("heatmap without size.field should fail validation")
	}
}

func ptr(f float64) *float64 { return &f }

func TestMetricCompositionAndChaining(t *testing.T) {
	// pressure = 0.4*a + 0.6*b; tens = pressure*10 references the earlier metric.
	s := Spec{
		Version: 1, Type: ChartLine,
		Data: DataRef{Source: "x", Kind: dataset.KindMetric},
		Metrics: []Metric{
			{Name: "pressure", Terms: []Term{{Field: "a", Weight: ptr(0.4)}, {Field: "b", Weight: ptr(0.6)}}},
			{Name: "tens", Terms: []Term{{Field: "pressure", Weight: ptr(10)}}},
		},
		X: Encoding{Field: "time"},
		Y: []Encoding{{Field: "pressure"}, {Field: "tens"}},
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	recs, _ := dataset.ReadJSONL(strings.NewReader(
		`{"time":"t1","a":10,"b":10}` + "\n" +
			`{"time":"t2","a":0,"b":0}` + "\n"))
	res := s.Resolve(recs)
	if got := res.Series[0].Values[0]; got != 10 { // 0.4*10+0.6*10
		t.Fatalf("pressure = %v, want 10", got)
	}
	if got := res.Series[1].Values[0]; got != 100 { // chained metric: pressure*10
		t.Fatalf("tens = %v, want 100", got)
	}
}

func TestMetricMissingFieldIsNaN(t *testing.T) {
	s := Spec{
		Version: 1, Type: ChartLine, Data: DataRef{Source: "x", Kind: dataset.KindMetric},
		Metrics: []Metric{{Name: "m", Terms: []Term{{Field: "a"}, {Field: "b"}}}}, // weights default to 1
		X:       Encoding{Field: "time"}, Y: []Encoding{{Field: "m"}},
	}
	recs, _ := dataset.ReadJSONL(strings.NewReader(
		`{"time":"t1","a":2,"b":3}` + "\n" + `{"time":"t2","a":2}` + "\n")) // b missing → NaN
	res := s.Resolve(recs)
	if res.Series[0].Values[0] != 5 { // default weight 1: 2+3
		t.Fatalf("m = %v, want 5", res.Series[0].Values[0])
	}
	if !math.IsNaN(res.Series[0].Values[1]) {
		t.Fatal("missing term should make metric NaN")
	}
}

func TestSeriesTransformApplied(t *testing.T) {
	s := Spec{
		Version: 1, Type: ChartLine, Data: DataRef{Source: "x", Kind: dataset.KindMetric},
		X: Encoding{Field: "time"},
		Y: []Encoding{{Field: "v", Transform: &Transform{Op: "sma", Window: 2}}},
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	recs, _ := dataset.ReadJSONL(strings.NewReader(
		`{"time":"t1","v":2}` + "\n" + `{"time":"t2","v":4}` + "\n" + `{"time":"t3","v":6}` + "\n"))
	res := s.Resolve(recs)
	vals := res.Series[0].Values
	if !math.IsNaN(vals[0]) || vals[1] != 3 || vals[2] != 5 { // SMA window 2: NaN,(2+4)/2,(4+6)/2
		t.Fatalf("sma values = %v, want [NaN 3 5]", vals)
	}
}

func TestTransformValidation(t *testing.T) {
	base := func(y Encoding, typ ChartType) Spec {
		s := Spec{Version: 1, Type: typ, Data: DataRef{Source: "x", Kind: dataset.KindMetric}, X: Encoding{Field: "t"}, Y: []Encoding{y}}
		if typ == ChartBubble || typ == ChartHeatmap {
			s.Size = &Encoding{Field: "s"}
		}
		return s
	}
	cases := map[string]Spec{
		"unknown op":       base(Encoding{Field: "v", Transform: &Transform{Op: "median"}}, ChartLine),
		"sma needs window": base(Encoding{Field: "v", Transform: &Transform{Op: "sma"}}, ChartLine),
		"not on bubble":    base(Encoding{Field: "v", Transform: &Transform{Op: "diff"}}, ChartBubble),
	}
	for name, s := range cases {
		if err := s.Validate(); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
}

func TestMetricValidation(t *testing.T) {
	base := Spec{Version: 1, Type: ChartLine, Data: DataRef{Source: "x", Kind: dataset.KindMetric}, X: Encoding{Field: "t"}, Y: []Encoding{{Field: "v"}}}
	noTerms := base
	noTerms.Metrics = []Metric{{Name: "m"}}
	noName := base
	noName.Metrics = []Metric{{Terms: []Term{{Field: "a"}}}}
	for name, s := range map[string]Spec{"no terms": noTerms, "no name": noName} {
		if err := s.Validate(); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
}

func TestTableResolveProjectsAndAligns(t *testing.T) {
	tbl := Table{
		Data:    DataRef{Source: "x", Kind: dataset.KindPrice},
		Columns: []Column{{Field: "entity", Label: "Sym"}, {Field: "close"}},
		Filter:  true,
	}
	recs, _ := dataset.ReadJSONL(strings.NewReader(
		`{"entity":"AAPL","close":10}` + "\n" + `{"entity":"MSFT"}` + "\n"))
	res := tbl.Resolve(recs)
	if want := []string{"Sym", "close"}; !equalStrings(res.Columns, want) {
		t.Fatalf("columns = %v, want %v", res.Columns, want)
	}
	if !res.Filter {
		t.Fatal("filter flag should carry through")
	}
	if got := res.Rows[0]; got[0] != "AAPL" || got[1] != "10" {
		t.Fatalf("row 0 = %v", got)
	}
	// Missing cell stays empty so the row still aligns to the header.
	if got := res.Rows[1]; got[0] != "MSFT" || got[1] != "" {
		t.Fatalf("row 1 = %v", got)
	}
}

func TestTableValidateErrors(t *testing.T) {
	cases := map[string]Table{
		"no source":   {Data: DataRef{Kind: dataset.KindPrice}, Columns: []Column{{Field: "c"}}},
		"bad kind":    {Data: DataRef{Source: "x", Kind: "bogus"}, Columns: []Column{{Field: "c"}}},
		"no columns":  {Data: DataRef{Source: "x", Kind: dataset.KindPrice}},
		"empty field": {Data: DataRef{Source: "x", Kind: dataset.KindPrice}, Columns: []Column{{Field: ""}}},
	}
	for name, tbl := range cases {
		if err := tbl.Validate(); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestResolveMissingYBecomesNaN(t *testing.T) {
	s, _ := Load(strings.NewReader(`{"version":1,"type":"line","data":{"source":"x","kind":"metric"},"x":{"field":"time"},"y":[{"field":"value"}]}`))
	recs, _ := dataset.ReadJSONL(strings.NewReader(
		`{"time":"d1","value":5}` + "\n" + `{"time":"d2"}` + "\n"))
	res := s.Resolve(recs)
	if res.Series[0].Values[0] != 5 {
		t.Fatalf("first value = %v", res.Series[0].Values[0])
	}
	if !math.IsNaN(res.Series[0].Values[1]) {
		t.Fatalf("missing value should be NaN, got %v", res.Series[0].Values[1])
	}
}

func TestOverlayMarkersDefaultsAndSkips(t *testing.T) {
	// At/Label unset → defaults to "time"/"text"; a record without the at field is skipped.
	ov := Overlay{Source: "ann.jsonl", Kind: dataset.KindAnnotation}
	recs, _ := dataset.ReadJSONL(strings.NewReader(
		`{"time":"d1","text":"hello"}` + "\n" + `{"text":"no time here"}` + "\n"))
	ms := ov.Markers(recs)
	if len(ms) != 1 || ms[0].At != "d1" || ms[0].Label != "hello" {
		t.Fatalf("markers = %+v", ms)
	}
}

func TestOverlayMarkersCustomFields(t *testing.T) {
	ov := Overlay{Source: "events.jsonl", Kind: dataset.KindEvent, At: "time", Label: "title"}
	recs, _ := dataset.ReadJSONL(strings.NewReader(`{"time":"d2","title":"tariff"}`))
	ms := ov.Markers(recs)
	if len(ms) != 1 || ms[0].Label != "tariff" {
		t.Fatalf("markers = %+v", ms)
	}
}

func TestResolveAssignsAxis(t *testing.T) {
	s, _ := Load(strings.NewReader(`{"version":1,"type":"line","data":{"source":"x","kind":"price"},` +
		`"x":{"field":"time"},"y":[{"field":"close"},{"field":"vix","axis":"y2"}]}`))
	recs, _ := dataset.ReadJSONL(strings.NewReader(`{"time":"d1","close":1,"vix":2}`))
	res := s.Resolve(recs)
	if res.Series[0].Axis != "y" {
		t.Fatalf("default axis = %q, want y", res.Series[0].Axis)
	}
	if res.Series[1].Axis != "y2" {
		t.Fatalf("explicit axis = %q, want y2", res.Series[1].Axis)
	}
}

func TestResolveBubbleBuildsPoints(t *testing.T) {
	s, _ := Load(strings.NewReader(`{"version":1,"type":"bubble","data":{"source":"x","kind":"metric"},` +
		`"x":{"field":"ret"},"y":[{"field":"vol"}],"size":{"field":"sz"}}`))
	recs, _ := dataset.ReadJSONL(strings.NewReader(
		`{"ret":12,"vol":40,"sz":1000}` + "\n" + `{"ret":-5,"vol":18,"sz":600}` + "\n"))
	res := s.Resolve(recs)
	if len(res.Labels) != 0 {
		t.Fatalf("bubble should not produce category labels, got %v", res.Labels)
	}
	if len(res.Series) != 1 || len(res.Series[0].Points) != 2 {
		t.Fatalf("series/points = %+v", res.Series)
	}
	p := res.Series[0].Points[0]
	if p.X != 12 || p.Y != 40 || p.R != 1000 {
		t.Fatalf("point = %+v", p)
	}
}

func TestValidateBubbleRequiresSize(t *testing.T) {
	s := Spec{
		Version: 1, Type: ChartBubble,
		Data: DataRef{Source: "x", Kind: dataset.KindMetric},
		X:    Encoding{Field: "ret"}, Y: []Encoding{{Field: "vol"}},
	}
	if err := s.Validate(); err == nil || !strings.Contains(err.Error(), "size") {
		t.Fatalf("want size error, got %v", err)
	}
}

func TestValidateRejectsBadAxis(t *testing.T) {
	s := Spec{
		Version: 1, Type: ChartLine,
		Data: DataRef{Source: "x", Kind: dataset.KindPrice},
		X:    Encoding{Field: "time"}, Y: []Encoding{{Field: "close", Axis: "y3"}},
	}
	if err := s.Validate(); err == nil || !strings.Contains(err.Error(), "axis") {
		t.Fatalf("want axis error, got %v", err)
	}
}

func TestValidateRejectsBadOverlay(t *testing.T) {
	s := Spec{
		Version: 1, Type: ChartLine,
		Data: DataRef{Source: "x", Kind: dataset.KindMetric},
		X:    Encoding{Field: "time"}, Y: []Encoding{{Field: "value"}},
		Overlays: []Overlay{{Kind: dataset.KindEvent}}, // missing source
	}
	if err := s.Validate(); err == nil || !strings.Contains(err.Error(), "overlays[0].source") {
		t.Fatalf("want overlay source error, got %v", err)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

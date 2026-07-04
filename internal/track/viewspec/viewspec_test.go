package viewspec

import (
	"math"
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/track/dataset"
)

const goodSpec = `{
  "version": 2,
  "mark": "line",
  "title": "AAPL",
  "data": {"source": "prices.jsonl", "kind": "price"},
  "encoding": {
    "x": {"field": "time"},
    "y": [{"field": "close", "title": "Close"}]
  },
  "filter": {"field": "entity", "equals": "AAPL"}
}`

func TestLoadValid(t *testing.T) {
	s, err := Load(strings.NewReader(goodSpec))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.Mark != MarkLine || s.Data.Kind != dataset.KindPrice || len(s.Encoding.Y) != 1 {
		t.Fatalf("unexpected spec: %+v", s)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	_, err := Load(strings.NewReader(`{"version":2,"mark":"line","data":{"source":"x","kind":"price"},"encoding":{"x":{"field":"t"},"y":[{"field":"c"}]},"bogus":1}`))
	if err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("want unknown-field error, got %v", err)
	}
}

// lineSpec builds a minimal valid line spec, so validation cases can vary one field at a time.
func lineSpec() Spec {
	return Spec{
		Version: Version, Mark: MarkLine,
		Data:     DataRef{Source: "x", Kind: dataset.KindPrice},
		Encoding: Encoding{X: Channel{Field: "t"}, Y: []Channel{{Field: "c"}}},
	}
}

func TestValidateErrors(t *testing.T) {
	noVer := lineSpec()
	noVer.Version = 0
	badMark := lineSpec()
	badMark.Mark = "pie"
	badKind := lineSpec()
	badKind.Data.Kind = "bogus"
	noSource := lineSpec()
	noSource.Data.Source = ""
	noX := lineSpec()
	noX.Encoding.X = Channel{}
	noY := lineSpec()
	noY.Encoding.Y = nil
	oldVer := lineSpec()
	oldVer.Version = 1
	future := lineSpec()
	future.Version = Version + 1
	badChanType := lineSpec()
	badChanType.Encoding.X.Type = "ordinal"

	cases := map[string]Spec{
		"missing version":  noVer,
		"bad mark":         badMark,
		"bad kind":         badKind,
		"no source":        noSource,
		"no x":             noX,
		"no y":             noY,
		"v1 no longer ok":  oldVer,
		"future version":   future,
		"bad channel type": badChanType,
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
	if res.Chart != ChartLine {
		t.Fatalf("chart form = %q, want line", res.Chart)
	}
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
		Version: Version, Mark: MarkRect,
		Data: DataRef{Source: "x", Kind: dataset.KindMetric},
		Encoding: Encoding{
			X:     Channel{Field: "col", Type: Nominal},
			Y:     []Channel{{Field: "row", Type: Nominal}},
			Color: &Channel{Field: "v"},
		},
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	recs, _ := dataset.ReadJSONL(strings.NewReader(
		`{"col":"Q1","row":"Tech","v":1}` + "\n" +
			`{"col":"Q2","row":"Tech","v":9}` + "\n" +
			`{"col":"Q1","row":"Energy"}` + "\n")) // missing value → NaN cell
	res := s.Resolve(recs)
	if res.Chart != ChartHeatmap || res.Grid == nil {
		t.Fatalf("rect mark should resolve to a heatmap grid, got %q", res.Chart)
	}
	if !equalStrings(res.Grid.Cols, []string{"Q1", "Q2"}) || !equalStrings(res.Grid.Rows, []string{"Tech", "Energy"}) {
		t.Fatalf("cols/rows = %v / %v", res.Grid.Cols, res.Grid.Rows)
	}
	if len(res.Grid.Cells) != 3 || res.Grid.Cells[1].Value != 9 {
		t.Fatalf("cells = %+v", res.Grid.Cells)
	}
	if !math.IsNaN(res.Grid.Cells[2].Value) {
		t.Fatal("missing color value should be NaN cell")
	}
}

func TestHeatmapRequiresColor(t *testing.T) {
	s := Spec{
		Version: Version, Mark: MarkRect,
		Data:     DataRef{Source: "x", Kind: dataset.KindMetric},
		Encoding: Encoding{X: Channel{Field: "c", Type: Nominal}, Y: []Channel{{Field: "r", Type: Nominal}}},
	}
	if err := s.Validate(); err == nil || !strings.Contains(err.Error(), "color") {
		t.Fatalf("rect without color.field should fail validation, got %v", err)
	}
}

func TestResolveTimelineGridUsesSize(t *testing.T) {
	s := Spec{
		Version: Version, Mark: MarkPoint,
		Data: DataRef{Source: "x", Kind: dataset.KindMetric},
		Encoding: Encoding{
			X:    Channel{Field: "time", Type: Nominal},
			Y:    []Channel{{Field: "lane", Type: Nominal}},
			Size: &Channel{Field: "v"},
		},
	}
	recs, _ := dataset.ReadJSONL(strings.NewReader(
		`{"time":"1","lane":"A","v":3}` + "\n" + `{"time":"2","lane":"B","v":5}` + "\n"))
	res := s.Resolve(recs)
	if res.Chart != ChartTimeline || res.Grid == nil {
		t.Fatalf("point mark with nominal y should be a timeline grid, got %q", res.Chart)
	}
	if len(res.Grid.Cells) != 2 || res.Grid.Cells[0].Value != 3 {
		t.Fatalf("cells = %+v", res.Grid.Cells)
	}
}

func TestLoadInlineRecordsResolve(t *testing.T) {
	spec := `{"version":2,"mark":"line","data":{"kind":"metric","records":[
		{"time":"d1","close":10},{"time":"d2","close":12}]},
		"encoding":{"x":{"field":"time"},"y":[{"field":"close"}]}}`
	s, err := Load(strings.NewReader(spec))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	res := s.Resolve(s.Data.Records)
	if !equalStrings(res.Labels, []string{"d1", "d2"}) {
		t.Fatalf("labels = %v", res.Labels)
	}
	if res.Series[0].Values[0] != 10 || res.Series[0].Values[1] != 12 {
		t.Fatalf("values = %v", res.Series[0].Values)
	}
}

func TestDataSourceRecordsMutuallyExclusive(t *testing.T) {
	base := func(d DataRef) Spec {
		return Spec{Version: Version, Mark: MarkLine, Data: d, Encoding: Encoding{X: Channel{Field: "t"}, Y: []Channel{{Field: "c"}}}}
	}
	rec := []dataset.Record{{"t": "d1", "c": 1.0}}
	// neither source nor records → error
	if err := base(DataRef{Kind: dataset.KindMetric}).Validate(); err == nil {
		t.Error("expected error when neither source nor records is set")
	}
	// both → error
	if err := base(DataRef{Kind: dataset.KindMetric, Source: "x.jsonl", Records: rec}).Validate(); err == nil {
		t.Error("expected error when both source and records are set")
	}
	// records only → ok
	if err := base(DataRef{Kind: dataset.KindMetric, Records: rec}).Validate(); err != nil {
		t.Errorf("records-only should validate: %v", err)
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
	s, _ := Load(strings.NewReader(`{"version":2,"mark":"line","data":{"source":"x","kind":"metric"},"encoding":{"x":{"field":"time"},"y":[{"field":"value"}]}}`))
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
	s, _ := Load(strings.NewReader(`{"version":2,"mark":"line","data":{"source":"x","kind":"price"},` +
		`"encoding":{"x":{"field":"time"},"y":[{"field":"close"},{"field":"vix","axis":"y2"}]}}`))
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
	s, _ := Load(strings.NewReader(`{"version":2,"mark":"point","data":{"source":"x","kind":"metric"},` +
		`"encoding":{"x":{"field":"ret"},"y":[{"field":"vol"}],"size":{"field":"sz"}}}`))
	recs, _ := dataset.ReadJSONL(strings.NewReader(
		`{"ret":12,"vol":40,"sz":1000}` + "\n" + `{"ret":-5,"vol":18,"sz":600}` + "\n"))
	res := s.Resolve(recs)
	if res.Chart != ChartBubble {
		t.Fatalf("point with quantitative x should be a bubble, got %q", res.Chart)
	}
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

func TestPointNominalXResolvesToScatter(t *testing.T) {
	s, _ := Load(strings.NewReader(`{"version":2,"mark":"point","data":{"source":"x","kind":"metric"},` +
		`"encoding":{"x":{"field":"name","type":"nominal"},"y":[{"field":"value"}]}}`))
	recs, _ := dataset.ReadJSONL(strings.NewReader(
		`{"name":"A","value":2}` + "\n" + `{"name":"B","value":6}` + "\n"))
	res := s.Resolve(recs)
	if res.Chart != ChartScatter {
		t.Fatalf("point with nominal x should be a scatter, got %q", res.Chart)
	}
	if !equalStrings(res.Labels, []string{"A", "B"}) || res.Series[0].Values[1] != 6 {
		t.Fatalf("scatter resolve = labels %v series %+v", res.Labels, res.Series)
	}
}

func TestResolveHorizontalBarSwapsAxes(t *testing.T) {
	// bar + nominal y = horizontal bar: the category comes from y, the measure from x.
	s, _ := Load(strings.NewReader(`{"version":2,"mark":"bar","data":{"source":"x","kind":"metric"},` +
		`"encoding":{"x":{"field":"value"},"y":[{"field":"name","type":"nominal"}]}}`))
	recs, _ := dataset.ReadJSONL(strings.NewReader(
		`{"name":"A","value":8}` + "\n" + `{"name":"B","value":3}` + "\n"))
	res := s.Resolve(recs)
	if res.Chart != ChartHBar {
		t.Fatalf("bar with nominal y should be hbar, got %q", res.Chart)
	}
	if !equalStrings(res.Labels, []string{"A", "B"}) {
		t.Fatalf("hbar labels (categories from y) = %v", res.Labels)
	}
	if len(res.Series) != 1 || res.Series[0].Values[0] != 8 || res.Series[0].Values[1] != 3 {
		t.Fatalf("hbar series (measure from x) = %+v", res.Series)
	}
}

func TestResolveColorSplitsSeries(t *testing.T) {
	// A nominal color splits records into one series per category, aligned to a shared x axis with
	// NaN gaps where a category has no record.
	s, err := Load(strings.NewReader(`{"version":2,"mark":"line","data":{"source":"x","kind":"metric"},` +
		`"encoding":{"x":{"field":"time"},"y":[{"field":"value"}],"color":{"field":"entity","type":"nominal"}}}`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	recs, _ := dataset.ReadJSONL(strings.NewReader(
		`{"name":"m","time":"d1","entity":"A","value":1}` + "\n" +
			`{"name":"m","time":"d1","entity":"B","value":10}` + "\n" +
			`{"name":"m","time":"d2","entity":"A","value":2}` + "\n" +
			`{"name":"m","time":"d3","entity":"B","value":30}` + "\n"))
	res := s.Resolve(recs)
	if !equalStrings(res.Labels, []string{"d1", "d2", "d3"}) {
		t.Fatalf("labels = %v", res.Labels)
	}
	if len(res.Series) != 2 || res.Series[0].Label != "A" || res.Series[1].Label != "B" {
		t.Fatalf("series = %+v", res.Series)
	}
	a, b := res.Series[0].Values, res.Series[1].Values
	if a[0] != 1 || a[1] != 2 || !math.IsNaN(a[2]) {
		t.Fatalf("series A = %v (want [1 2 NaN])", a)
	}
	if b[0] != 10 || !math.IsNaN(b[1]) || b[2] != 30 {
		t.Fatalf("series B = %v (want [10 NaN 30])", b)
	}
}

func TestResolveColorHorizontalBar(t *testing.T) {
	// hbar keeps its nominal-y categories as labels; color splits the measure into grouped series.
	s, _ := Load(strings.NewReader(`{"version":2,"mark":"bar","data":{"source":"x","kind":"metric"},` +
		`"encoding":{"x":{"field":"value"},"y":[{"field":"name","type":"nominal"}],"color":{"field":"entity","type":"nominal"}}}`))
	recs, _ := dataset.ReadJSONL(strings.NewReader(
		`{"name":"r1","entity":"A","value":8}` + "\n" +
			`{"name":"r1","entity":"B","value":5}` + "\n" +
			`{"name":"r2","entity":"A","value":3}` + "\n"))
	res := s.Resolve(recs)
	if res.Chart != ChartHBar {
		t.Fatalf("chart = %q, want hbar", res.Chart)
	}
	if !equalStrings(res.Labels, []string{"r1", "r2"}) {
		t.Fatalf("labels = %v", res.Labels)
	}
	if len(res.Series) != 2 || res.Series[0].Label != "A" || res.Series[1].Label != "B" {
		t.Fatalf("series = %+v", res.Series)
	}
	if res.Series[0].Values[0] != 8 || res.Series[0].Values[1] != 3 || !math.IsNaN(res.Series[1].Values[1]) {
		t.Fatalf("values = %v / %v", res.Series[0].Values, res.Series[1].Values)
	}
}

func TestResolveColorBubbleSplitsPoints(t *testing.T) {
	s, _ := Load(strings.NewReader(`{"version":2,"mark":"point","data":{"source":"x","kind":"metric"},` +
		`"encoding":{"x":{"field":"ret"},"y":[{"field":"vol"}],"size":{"field":"sz"},"color":{"field":"entity","type":"nominal"}}}`))
	recs, _ := dataset.ReadJSONL(strings.NewReader(
		`{"name":"m","entity":"A","ret":12,"vol":40,"sz":100}` + "\n" +
			`{"name":"m","entity":"B","ret":-5,"vol":18,"sz":60}` + "\n" +
			`{"name":"m","entity":"A","ret":3,"vol":25,"sz":80}` + "\n"))
	res := s.Resolve(recs)
	if res.Chart != ChartBubble {
		t.Fatalf("chart = %q, want bubble", res.Chart)
	}
	if len(res.Series) != 2 || res.Series[0].Label != "A" || res.Series[1].Label != "B" {
		t.Fatalf("series = %+v", res.Series)
	}
	if len(res.Series[0].Points) != 2 || len(res.Series[1].Points) != 1 {
		t.Fatalf("points split = %d/%d, want 2/1", len(res.Series[0].Points), len(res.Series[1].Points))
	}
	if p := res.Series[0].Points[1]; p.X != 3 || p.Y != 25 || p.R != 80 {
		t.Fatalf("point = %+v", p)
	}
}

func TestValidateColorConstraints(t *testing.T) {
	base := func() Spec {
		s := lineSpec()
		s.Encoding.Color = &Channel{Field: "entity", Type: Nominal}
		return s
	}
	if err := base().Validate(); err != nil {
		t.Fatalf("nominal color on line should validate: %v", err)
	}
	quant := base()
	quant.Encoding.Color.Type = "" // default quantitative
	if err := quant.Validate(); err == nil || !strings.Contains(err.Error(), "nominal") {
		t.Fatalf("non-nominal color on line should fail, got %v", err)
	}
	multiY := base()
	multiY.Encoding.Y = append(multiY.Encoding.Y, Channel{Field: "d"})
	if err := multiY.Validate(); err == nil || !strings.Contains(err.Error(), "single encoding.y") {
		t.Fatalf("color with multiple y should fail, got %v", err)
	}
	timeline := base()
	timeline.Mark = MarkPoint
	timeline.Encoding.X.Type = Nominal
	timeline.Encoding.Y[0].Type = Nominal
	if err := timeline.Validate(); err == nil || !strings.Contains(err.Error(), "timeline") {
		t.Fatalf("color on timeline should fail, got %v", err)
	}
}

func TestBubbleWithoutSizeIsValid(t *testing.T) {
	// A point on quantitative axes with no size is a plain linear scatter — allowed (uniform radius).
	s := Spec{
		Version: Version, Mark: MarkPoint,
		Data:     DataRef{Source: "x", Kind: dataset.KindMetric},
		Encoding: Encoding{X: Channel{Field: "ret"}, Y: []Channel{{Field: "vol"}}},
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("point without size should validate: %v", err)
	}
}

func TestValidateRejectsBadAxis(t *testing.T) {
	s := Spec{
		Version: Version, Mark: MarkLine,
		Data:     DataRef{Source: "x", Kind: dataset.KindPrice},
		Encoding: Encoding{X: Channel{Field: "time"}, Y: []Channel{{Field: "close", Axis: "y3"}}},
	}
	if err := s.Validate(); err == nil || !strings.Contains(err.Error(), "axis") {
		t.Fatalf("want axis error, got %v", err)
	}
}

func TestValidateRejectsBadOverlay(t *testing.T) {
	s := Spec{
		Version: Version, Mark: MarkLine,
		Data:     DataRef{Source: "x", Kind: dataset.KindMetric},
		Encoding: Encoding{X: Channel{Field: "time"}, Y: []Channel{{Field: "value"}}},
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

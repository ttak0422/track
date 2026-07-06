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
	recs, _ := dataset.ReadJSONL(strings.NewReader(`{"time":"d2","title":"launch"}`))
	ms := ov.Markers(recs)
	if len(ms) != 1 || ms[0].Label != "launch" {
		t.Fatalf("markers = %+v", ms)
	}
}

func TestOverlayMarkersCarryProvenance(t *testing.T) {
	// The event kind's url/note fields ride along automatically — no extra vocabulary.
	ov := Overlay{Source: "events.jsonl", Kind: dataset.KindEvent, At: "time", Label: "title"}
	recs, _ := dataset.ReadJSONL(strings.NewReader(
		`{"time":"d1","title":"launch","url":"https://example.com/a","note":"1700000000000"}` + "\n" +
			`{"time":"d2","title":"plain"}` + "\n"))
	ms := ov.Markers(recs)
	if len(ms) != 2 {
		t.Fatalf("markers = %+v", ms)
	}
	if ms[0].Href != "https://example.com/a" || ms[0].Note != "1700000000000" {
		t.Fatalf("provenance should ride along: %+v", ms[0])
	}
	if ms[1].Href != "" || ms[1].Note != "" {
		t.Fatalf("a plain event carries no provenance: %+v", ms[1])
	}
}

func TestValidateDetailAndHrefChannels(t *testing.T) {
	noField := lineSpec()
	noField.Encoding.Detail = []Channel{{Title: "x"}}
	if err := noField.Validate(); err == nil || !strings.Contains(err.Error(), "encoding.detail[0].field") {
		t.Fatalf("detail without a field should error, got %v", err)
	}
	noHrefField := lineSpec()
	noHrefField.Encoding.Href = &Channel{}
	if err := noHrefField.Validate(); err == nil || !strings.Contains(err.Error(), "encoding.href.field") {
		t.Fatalf("href without a field should error, got %v", err)
	}
	misplacedSort := lineSpec()
	misplacedSort.Encoding.Href = &Channel{Field: "url", Sort: "ascending"}
	if err := misplacedSort.Validate(); err == nil || !strings.Contains(err.Error(), "encoding.href") {
		t.Fatalf("sort on href should be rejected, got %v", err)
	}
	// Grid forms reshape records instead of aligning them per label, so extras have no slot yet.
	heat := Spec{
		Version: Version, Mark: MarkRect,
		Data: DataRef{Source: "x", Kind: dataset.KindMetric},
		Encoding: Encoding{
			X:     Channel{Field: "col", Type: Nominal},
			Y:     []Channel{{Field: "row", Type: Nominal}},
			Color: &Channel{Field: "v"},
			Href:  &Channel{Field: "url"},
		},
	}
	if err := heat.Validate(); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("href on a heatmap should be rejected, got %v", err)
	}
}

func TestResolveCarriesPointExtras(t *testing.T) {
	s, err := Load(strings.NewReader(`{"version":2,"mark":"point","data":{"source":"x","kind":"event"},` +
		`"encoding":{"x":{"field":"time","type":"nominal"},"y":[{"field":"amount"}],` +
		`"detail":[{"field":"title","title":"what"},{"field":"amount"}],"href":{"field":"url"},"note":{"field":"ref"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	recs, _ := dataset.ReadJSONL(strings.NewReader(
		`{"time":"d1","title":"buy","amount":120,"url":"https://example.com/t1","ref":"1700000000000"}` + "\n" +
			`{"time":"d2","title":"sell","amount":80}` + "\n"))
	res := s.Resolve(recs)
	ex := res.Series[0].Extras
	if len(ex) != 2 {
		t.Fatalf("extras should align with values: %+v", ex)
	}
	if ex[0].Href != "https://example.com/t1" || ex[0].Note != "1700000000000" {
		t.Fatalf("href/note channels should read the record: %+v", ex[0])
	}
	if len(ex[0].Detail) != 2 || ex[0].Detail[0] != (KV{Label: "what", Value: "buy"}) || ex[0].Detail[1] != (KV{Label: "amount", Value: "120"}) {
		t.Fatalf("detail rows should be labelled by channel title and keep source precision: %+v", ex[0].Detail)
	}
	if ex[1].Href != "" {
		t.Fatalf("a record without the href field carries none: %+v", ex[1])
	}
}

func TestResolveExtrasFollowSortAndColorSplit(t *testing.T) {
	s, err := Load(strings.NewReader(`{"version":2,"mark":"bar","data":{"source":"x","kind":"event"},` +
		`"encoding":{"x":{"field":"who","type":"nominal","sort":"ascending"},"y":[{"field":"amount"}],` +
		`"color":{"field":"side","type":"nominal"},"href":{"field":"url"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	recs, _ := dataset.ReadJSONL(strings.NewReader(
		`{"who":"b","side":"buy","amount":2,"url":"https://example.com/b"}` + "\n" +
			`{"who":"a","side":"buy","amount":1,"url":"https://example.com/a"}` + "\n" +
			`{"who":"a","side":"sell","amount":3,"url":"https://example.com/as"}` + "\n"))
	res := s.Resolve(recs)
	// After the ascending sort the labels are a,b; extras must have moved with their values.
	if !equalStrings(res.Labels, []string{"a", "b"}) {
		t.Fatalf("labels = %v", res.Labels)
	}
	buy := res.Series[0]
	if buy.Extras[0].Href != "https://example.com/a" || buy.Extras[1].Href != "https://example.com/b" {
		t.Fatalf("buy extras should follow the sort: %+v", buy.Extras)
	}
	sell := res.Series[1]
	if sell.Extras[0].Href != "https://example.com/as" || sell.Extras[1].Href != "" {
		t.Fatalf("sell extras should align with the NaN gap: %+v", sell.Extras)
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

func TestValidateOverlayShapes(t *testing.T) {
	spec := func(o Overlay) Spec {
		return Spec{
			Version: Version, Mark: MarkLine,
			Data:     DataRef{Source: "x", Kind: dataset.KindMetric},
			Encoding: Encoding{X: Channel{Field: "time"}, Y: []Channel{{Field: "value"}}},
			Overlays: []Overlay{o},
		}
	}
	y := 6.5
	inline := []dataset.Record{{"time": "d1", "title": "ev"}}
	valid := map[string]Overlay{
		"callout":        {X: "d1", Y: &y, Label: "peak here"},
		"markers":        {Source: "events.jsonl", Kind: dataset.KindEvent, At: "time", Label: "title"},
		"inline markers": {Records: inline, Kind: dataset.KindEvent, At: "time", Label: "title"},
		"line":           {Y: &y, Label: "threshold"},
		"line-y2":        {Y: &y, Axis: "y2"},
		"band":           {From: "d1", To: "d2", Label: "Q1"},
		"line-at-zero":   {Y: new(float64)}, // y: 0 is a value, not an unset shape
		"box markers":    {Records: inline, Kind: dataset.KindEvent, Label: "title", Display: "box"},
	}
	for name, o := range valid {
		if err := spec(o).Validate(); err != nil {
			t.Errorf("%s should validate: %v", name, err)
		}
	}
	invalid := map[string]Overlay{
		"no shape":             {Kind: dataset.KindEvent}, // kind without source names no shape
		"empty":                {},
		"line and band mixed":  {Y: &y, From: "d1", To: "d2"},
		"source and line":      {Source: "e.jsonl", Kind: dataset.KindEvent, Y: &y},
		"band missing to":      {From: "d1"},
		"band missing from":    {To: "d2"},
		"bad line axis":        {Y: &y, Axis: "y3"},
		"axis on markers":      {Source: "e.jsonl", Kind: dataset.KindEvent, Axis: "y"},
		"kind on line":         {Y: &y, Kind: dataset.KindEvent},
		"at on band":           {From: "d1", To: "d2", At: "time"},
		"markers bad kind":     {Source: "e.jsonl", Kind: "nope"},
		"source and records":   {Source: "e.jsonl", Records: inline, Kind: dataset.KindEvent},
		"callout missing y":    {X: "d1", Label: "t"},
		"callout missing text": {X: "d1", Y: &y},
		"callout with kind":    {X: "d1", Y: &y, Label: "t", Kind: dataset.KindEvent},
		"callout and band":     {X: "d1", Y: &y, Label: "t", From: "a", To: "b"},
		"bad inline record":    {Records: []dataset.Record{{"time": "d1"}}, Kind: dataset.KindEvent}, // missing required title
		"bad display value":    {Records: inline, Kind: dataset.KindEvent, Display: "boxx"},
		"display on line":      {Y: &y, Display: "box"},
		"display on band":      {From: "d1", To: "d2", Display: "box"},
		"display on callout":   {X: "d1", Y: &y, Label: "t", Display: "box"},
	}
	for name, o := range invalid {
		if err := spec(o).Validate(); err == nil || !strings.Contains(err.Error(), "overlays[0]") {
			t.Errorf("%s: want overlays[0] error, got %v", name, err)
		}
	}
}

// TestValidateDisplayBoxFormGate pins where the annotation-box mode may appear: the category-x
// series forms (candlestick included — markers anchor there today); the grid forms and hbar error.
func TestValidateDisplayBoxFormGate(t *testing.T) {
	overlay := `"overlays":[{"source":"e.jsonl","kind":"event","display":"box"}]`
	allowed := map[string]string{
		"line": `{"version":2,"mark":"line","data":{"source":"x","kind":"metric"},` +
			`"encoding":{"x":{"field":"time"},"y":[{"field":"value"}]},` + overlay + `}`,
		"candlestick": `{"version":2,"mark":"candlestick","data":{"source":"x","kind":"price"},` +
			`"encoding":{"x":{"field":"time"}},` + overlay + `}`,
	}
	for name, s := range allowed {
		if _, err := Load(strings.NewReader(s)); err != nil {
			t.Errorf("%s should allow display box: %v", name, err)
		}
	}
	gated := map[string]string{
		"hbar": `{"version":2,"mark":"bar","data":{"source":"x","kind":"metric"},` +
			`"encoding":{"x":{"field":"value"},"y":[{"field":"name","type":"nominal"}]},` + overlay + `}`,
		"heatmap": `{"version":2,"mark":"rect","data":{"source":"x","kind":"metric"},` +
			`"encoding":{"x":{"field":"c","type":"nominal"},"y":[{"field":"r","type":"nominal"}],"color":{"field":"v"}},` + overlay + `}`,
	}
	for name, s := range gated {
		if _, err := Load(strings.NewReader(s)); err == nil || !strings.Contains(err.Error(), "display") {
			t.Errorf("%s: want display form-gate error, got %v", name, err)
		}
	}
}

// TestMarkersStampBox pins that display: "box" rides onto every resolved Marker through the single
// extraction point, so all three Resolved.Markers fill sites inherit it.
func TestMarkersStampBox(t *testing.T) {
	recs := []dataset.Record{{"time": "d1", "title": "ev", "url": "https://example.com/n"}}
	o := Overlay{Kind: dataset.KindEvent, Label: "title", Display: "box"}
	ms := o.Markers(recs)
	if len(ms) != 1 || !ms[0].Box {
		t.Fatalf("display box should stamp Marker.Box: %+v", ms)
	}
	o.Display = ""
	if ms := o.Markers(recs); len(ms) != 1 || ms[0].Box {
		t.Fatalf("no display should leave Marker.Box unset: %+v", ms)
	}
}

func TestResolveFillsLinesAndBands(t *testing.T) {
	s, err := Load(strings.NewReader(`{"version":2,"mark":"line","data":{"source":"x","kind":"metric"},` +
		`"encoding":{"x":{"field":"time"},"y":[{"field":"value"}]},` +
		`"overlays":[{"y":6.5,"label":"limit"},{"y":1,"axis":"y2"},{"from":"d1","to":"d2","label":"Q1"},` +
		`{"source":"e.jsonl","kind":"event"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	recs, _ := dataset.ReadJSONL(strings.NewReader(`{"time":"d1","value":5}`))
	res := s.Resolve(recs)
	if len(res.Lines) != 2 || res.Lines[0] != (RefLine{Y: 6.5, Axis: "y", Label: "limit"}) || res.Lines[1].Axis != "y2" {
		t.Fatalf("lines = %+v", res.Lines)
	}
	if len(res.Bands) != 1 || res.Bands[0] != (Band{From: "d1", To: "d2", Label: "Q1"}) {
		t.Fatalf("bands = %+v", res.Bands)
	}
	// The source overlay resolves via Overlay.Markers with its own records, not here.
	if len(res.Markers) != 0 {
		t.Fatalf("markers should stay caller-filled, got %+v", res.Markers)
	}
}

func TestComboMarkOverrides(t *testing.T) {
	// A y channel's mark override composes bars and lines in one chart.
	s, err := Load(strings.NewReader(`{"version":2,"mark":"bar","data":{"source":"x","kind":"metric"},` +
		`"encoding":{"x":{"field":"time"},"y":[{"field":"value"},{"field":"index","mark":"line","axis":"y2"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	recs, _ := dataset.ReadJSONL(strings.NewReader(`{"name":"m","time":"d1","value":5,"index":40}`))
	res := s.Resolve(recs)
	if res.Chart != ChartBar {
		t.Fatalf("chart = %v", res.Chart)
	}
	if res.SeriesForm(0) != ChartBar || res.SeriesForm(1) != ChartLine {
		t.Fatalf("series forms = %v / %v", res.SeriesForm(0), res.SeriesForm(1))
	}

	invalid := map[string]string{
		"bad value":     `{"version":2,"mark":"bar","data":{"source":"x","kind":"metric"},"encoding":{"x":{"field":"t"},"y":[{"field":"v","mark":"rect"}]}}`,
		"on point mark": `{"version":2,"mark":"point","data":{"source":"x","kind":"metric"},"encoding":{"x":{"field":"t","type":"nominal"},"y":[{"field":"v","mark":"line"}]}}`,
		"on hbar":       `{"version":2,"mark":"bar","data":{"source":"x","kind":"metric"},"encoding":{"x":{"field":"v"},"y":[{"field":"t","type":"nominal","mark":"line"}]}}`,
		"with color":    `{"version":2,"mark":"bar","data":{"source":"x","kind":"metric"},"encoding":{"x":{"field":"t"},"y":[{"field":"v","mark":"line"}],"color":{"field":"c","type":"nominal"}}}`,
		"on x channel":  `{"version":2,"mark":"bar","data":{"source":"x","kind":"metric"},"encoding":{"x":{"field":"t","mark":"line"},"y":[{"field":"v"}]}}`,
	}
	for name, spec := range invalid {
		if _, err := Load(strings.NewReader(spec)); err == nil || !strings.Contains(err.Error(), "mark") {
			t.Errorf("%s: want mark placement error, got %v", name, err)
		}
	}
}

func TestResolveFillsCallouts(t *testing.T) {
	s, err := Load(strings.NewReader(`{"version":2,"mark":"line","data":{"source":"x","kind":"metric"},` +
		`"encoding":{"x":{"field":"time"},"y":[{"field":"value"}]},` +
		`"overlays":[{"x":"d2","y":7.5,"label":"peak"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	recs, _ := dataset.ReadJSONL(strings.NewReader(`{"time":"d2","value":7.5}`))
	res := s.Resolve(recs)
	if len(res.Callouts) != 1 || res.Callouts[0] != (Callout{X: "d2", Y: 7.5, Label: "peak"}) {
		t.Fatalf("callouts = %+v", res.Callouts)
	}
	// A callout carries a y but is not a reference line.
	if len(res.Lines) != 0 {
		t.Fatalf("callout must not double as a line: %+v", res.Lines)
	}
}

func TestResolveFillsInlineMarkerRecords(t *testing.T) {
	s, err := Load(strings.NewReader(`{"version":2,"mark":"line","data":{"source":"x","kind":"metric"},` +
		`"encoding":{"x":{"field":"time"},"y":[{"field":"value"}]},` +
		`"overlays":[{"records":[{"time":"d1","title":"launch"}],"kind":"event","label":"title"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	recs, _ := dataset.ReadJSONL(strings.NewReader(`{"time":"d1","value":5}`))
	res := s.Resolve(recs)
	if len(res.Markers) != 1 || res.Markers[0] != (Marker{At: "d1", Label: "launch"}) {
		t.Fatalf("markers = %+v", res.Markers)
	}
}

func TestResolveSortByLabel(t *testing.T) {
	// ascending is numeric-aware: "9" sorts before "100".
	s, _ := Load(strings.NewReader(`{"version":2,"mark":"bar","data":{"source":"x","kind":"metric"},` +
		`"encoding":{"x":{"field":"time","type":"nominal","sort":"ascending"},"y":[{"field":"value"}]}}`))
	recs, _ := dataset.ReadJSONL(strings.NewReader(
		`{"time":"100","value":1}` + "\n" + `{"time":"9","value":2}` + "\n" + `{"time":"30","value":3}` + "\n"))
	res := s.Resolve(recs)
	if !equalStrings(res.Labels, []string{"9", "30", "100"}) {
		t.Fatalf("labels = %v", res.Labels)
	}
	if v := res.Series[0].Values; v[0] != 2 || v[1] != 3 || v[2] != 1 {
		t.Fatalf("values moved with labels: %v", v)
	}
}

func TestResolveSortByValueDescendingAndLimit(t *testing.T) {
	// -value + limit = a top-N ranking; hbar's category axis is the nominal y.
	s, _ := Load(strings.NewReader(`{"version":2,"mark":"bar","data":{"source":"x","kind":"metric"},` +
		`"encoding":{"x":{"field":"value"},"y":[{"field":"name","type":"nominal","sort":"-value","limit":2}]}}`))
	recs, _ := dataset.ReadJSONL(strings.NewReader(
		`{"name":"low","value":1}` + "\n" + `{"name":"top","value":9}` + "\n" + `{"name":"mid","value":5}` + "\n"))
	res := s.Resolve(recs)
	if res.Chart != ChartHBar {
		t.Fatalf("chart = %q", res.Chart)
	}
	if !equalStrings(res.Labels, []string{"top", "mid"}) {
		t.Fatalf("labels = %v", res.Labels)
	}
	if v := res.Series[0].Values; len(v) != 2 || v[0] != 9 || v[1] != 5 {
		t.Fatalf("values = %v", v)
	}
}

func TestResolveSortByValueSumsSeries(t *testing.T) {
	// A value sort orders categories by their series total (NaN gaps ignored), so it composes with a
	// color split: B totals 10, A totals 3+4=7.
	s, _ := Load(strings.NewReader(`{"version":2,"mark":"line","data":{"source":"x","kind":"metric"},` +
		`"encoding":{"x":{"field":"time","sort":"value"},"y":[{"field":"value"}],` +
		`"color":{"field":"entity","type":"nominal"}}}`))
	recs, _ := dataset.ReadJSONL(strings.NewReader(
		`{"time":"d1","entity":"A","value":3}` + "\n" +
			`{"time":"d1","entity":"B","value":4}` + "\n" +
			`{"time":"d2","entity":"B","value":10}` + "\n"))
	res := s.Resolve(recs)
	if !equalStrings(res.Labels, []string{"d1", "d2"}) { // 7 < 10
		t.Fatalf("labels = %v", res.Labels)
	}
	desc := s
	desc.Encoding.X.Sort = "-value"
	res = desc.Resolve(recs)
	if !equalStrings(res.Labels, []string{"d2", "d1"}) {
		t.Fatalf("-value labels = %v", res.Labels)
	}
}

func TestResolveLimitWithoutSortKeepsFirstSeen(t *testing.T) {
	s, _ := Load(strings.NewReader(`{"version":2,"mark":"line","data":{"source":"x","kind":"metric"},` +
		`"encoding":{"x":{"field":"time","limit":1},"y":[{"field":"value"}]}}`))
	recs, _ := dataset.ReadJSONL(strings.NewReader(
		`{"time":"d1","value":1}` + "\n" + `{"time":"d2","value":2}` + "\n"))
	res := s.Resolve(recs)
	if !equalStrings(res.Labels, []string{"d1"}) || len(res.Series[0].Values) != 1 {
		t.Fatalf("limited resolve = %v / %v", res.Labels, res.Series[0].Values)
	}
}

func TestResolveStackedFlag(t *testing.T) {
	s, _ := Load(strings.NewReader(`{"version":2,"mark":"bar","data":{"source":"x","kind":"metric"},` +
		`"encoding":{"x":{"field":"time","type":"nominal"},"y":[{"field":"value","stack":true}],` +
		`"color":{"field":"entity","type":"nominal"}}}`))
	recs, _ := dataset.ReadJSONL(strings.NewReader(`{"time":"d1","entity":"A","value":1}`))
	if res := s.Resolve(recs); !res.Stacked {
		t.Fatal("stack on the bar's y should set Resolved.Stacked")
	}
	// hbar: the measure channel is x.
	h, _ := Load(strings.NewReader(`{"version":2,"mark":"bar","data":{"source":"x","kind":"metric"},` +
		`"encoding":{"x":{"field":"value","stack":true},"y":[{"field":"name","type":"nominal"}]}}`))
	if res := h.Resolve(recs); !res.Stacked {
		t.Fatal("stack on the hbar's x should set Resolved.Stacked")
	}
}

func TestValidateChannelOptionPlacement(t *testing.T) {
	load := func(spec string) error {
		_, err := Load(strings.NewReader(spec))
		return err
	}
	valid := map[string]string{
		"sort on line x": `{"version":2,"mark":"line","data":{"source":"x","kind":"metric"},` +
			`"encoding":{"x":{"field":"t","sort":"descending"},"y":[{"field":"v"}]}}`,
		"sort+limit on hbar y": `{"version":2,"mark":"bar","data":{"source":"x","kind":"metric"},` +
			`"encoding":{"x":{"field":"v"},"y":[{"field":"n","type":"nominal","sort":"-value","limit":3}]}}`,
		"stack on bar y": `{"version":2,"mark":"bar","data":{"source":"x","kind":"metric"},` +
			`"encoding":{"x":{"field":"t","type":"nominal"},"y":[{"field":"v","stack":true}]}}`,
	}
	for name, spec := range valid {
		if err := load(spec); err != nil {
			t.Errorf("%s should validate: %v", name, err)
		}
	}
	invalid := map[string]string{
		"unknown sort": `{"version":2,"mark":"line","data":{"source":"x","kind":"metric"},` +
			`"encoding":{"x":{"field":"t","sort":"sideways"},"y":[{"field":"v"}]}}`,
		"sort on measure y": `{"version":2,"mark":"line","data":{"source":"x","kind":"metric"},` +
			`"encoding":{"x":{"field":"t"},"y":[{"field":"v","sort":"ascending"}]}}`,
		"sort on bubble": `{"version":2,"mark":"point","data":{"source":"x","kind":"metric"},` +
			`"encoding":{"x":{"field":"a","sort":"ascending"},"y":[{"field":"b"}]}}`,
		"sort on heatmap": `{"version":2,"mark":"rect","data":{"source":"x","kind":"metric"},` +
			`"encoding":{"x":{"field":"c","type":"nominal","sort":"ascending"},"y":[{"field":"r","type":"nominal"}],"color":{"field":"v"}}}`,
		"negative limit": `{"version":2,"mark":"line","data":{"source":"x","kind":"metric"},` +
			`"encoding":{"x":{"field":"t","limit":-1},"y":[{"field":"v"}]}}`,
		"limit on color": `{"version":2,"mark":"line","data":{"source":"x","kind":"metric"},` +
			`"encoding":{"x":{"field":"t"},"y":[{"field":"v"}],"color":{"field":"e","type":"nominal","limit":2}}}`,
		"stack on line": `{"version":2,"mark":"line","data":{"source":"x","kind":"metric"},` +
			`"encoding":{"x":{"field":"t"},"y":[{"field":"v","stack":true}]}}`,
		"stack on bar category x": `{"version":2,"mark":"bar","data":{"source":"x","kind":"metric"},` +
			`"encoding":{"x":{"field":"t","type":"nominal","stack":true},"y":[{"field":"v"}]}}`,
	}
	for name, spec := range invalid {
		if err := load(spec); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestAreaResolvesToAreaChart(t *testing.T) {
	s, _ := Load(strings.NewReader(`{"version":2,"mark":"area","data":{"source":"x","kind":"metric"},` +
		`"encoding":{"x":{"field":"time"},"y":[{"field":"value"}]}}`))
	recs, _ := dataset.ReadJSONL(strings.NewReader(
		`{"time":"d1","value":1}` + "\n" + `{"time":"d2","value":3}` + "\n"))
	res := s.Resolve(recs)
	if res.Chart != ChartArea {
		t.Fatalf("area mark should resolve to chart area, got %q", res.Chart)
	}
	// Same series shape as a line, so every channel (color, sort, y2, …) keeps working.
	if !equalStrings(res.Labels, []string{"d1", "d2"}) || res.Series[0].Values[1] != 3 {
		t.Fatalf("area resolve = labels %v series %+v", res.Labels, res.Series)
	}
}

func TestCandlestickResolve(t *testing.T) {
	s, err := Load(strings.NewReader(`{"version":2,"mark":"candlestick","data":{"source":"x","kind":"price"},` +
		`"encoding":{"x":{"field":"time"}}}`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	recs, _ := dataset.ReadJSONL(strings.NewReader(
		`{"entity":"AAPL","time":"d1","open":10,"high":14,"low":9,"close":13}` + "\n" +
			`{"entity":"AAPL","time":"d2","open":13,"high":13,"low":8,"close":9}` + "\n"))
	res := s.Resolve(recs)
	if res.Chart != ChartCandlestick {
		t.Fatalf("chart = %q, want candlestick", res.Chart)
	}
	if !equalStrings(res.Labels, []string{"d1", "d2"}) {
		t.Fatalf("labels = %v", res.Labels)
	}
	if len(res.Series) != len(CandleSeries) {
		t.Fatalf("series = %+v", res.Series)
	}
	for i, want := range CandleSeries {
		if res.Series[i].Label != want {
			t.Fatalf("series %d = %q, want %q", i, res.Series[i].Label, want)
		}
	}
	// open/high/low/close of the second candle, in CandleSeries order.
	for i, want := range []float64{13, 13, 8, 9} {
		if got := res.Series[i].Values[1]; got != want {
			t.Fatalf("series %s[1] = %v, want %v", CandleSeries[i], got, want)
		}
	}
}

func TestCandlestickValidation(t *testing.T) {
	load := func(spec string) error {
		_, err := Load(strings.NewReader(spec))
		return err
	}
	if err := load(`{"version":2,"mark":"candlestick","data":{"source":"x","kind":"price"},` +
		`"encoding":{"x":{"field":"time","sort":"ascending"}}}`); err != nil {
		t.Fatalf("candlestick with sorted x should validate: %v", err)
	}
	invalid := map[string]string{
		"non-price kind": `{"version":2,"mark":"candlestick","data":{"source":"x","kind":"metric"},` +
			`"encoding":{"x":{"field":"time"}}}`,
		"y channel set": `{"version":2,"mark":"candlestick","data":{"source":"x","kind":"price"},` +
			`"encoding":{"x":{"field":"time"},"y":[{"field":"close"}]}}`,
		"color set": `{"version":2,"mark":"candlestick","data":{"source":"x","kind":"price"},` +
			`"encoding":{"x":{"field":"time"},"color":{"field":"entity","type":"nominal"}}}`,
		"size set": `{"version":2,"mark":"candlestick","data":{"source":"x","kind":"price"},` +
			`"encoding":{"x":{"field":"time"},"size":{"field":"volume"}}}`,
	}
	for name, spec := range invalid {
		if err := load(spec); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	// Other marks still require a y channel.
	if err := load(`{"version":2,"mark":"line","data":{"source":"x","kind":"metric"},` +
		`"encoding":{"x":{"field":"time"}}}`); err == nil {
		t.Error("line without y should still fail")
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

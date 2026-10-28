package metrics

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const liveDashboardJSON = `{"title":"Sectors","panels":[{"type":"timeseries","title":"Level","gridPos":{"x":0,"y":0,"w":12},"datasource":{"type":"track","uid":"sectors.jsonl"},"targets":[{"expr":"level{entity=\"A\"}","legendFormat":"{{entity}} level"}],"fieldConfig":{"defaults":{"unit":"pct","min":-100,"max":100}}}]}`

func liveMetricsFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	body := `{"version":1,"name":"level","entity":"A","time":"2026-10-01","value":-12,"unit":"pct","frequency":"weekly","scope":"sector","source":"fixture","url":"https://example.test/release","revision":"first"}
{"version":1,"name":"level","entity":"B","time":"2026-10-01","value":99}
{"version":1,"name":"level","entity":"A","time":"2026-10-08","value":null,"missing_reason":"not published","unit":"pct","frequency":"weekly"}
{"version":1,"name":"level","entity":"A","time":"2026-10-15","value":-10,"unit":"pct","frequency":"weekly","scope":"sector","source":"fixture","revision":"second"}
`
	if err := os.WriteFile(filepath.Join(dir, "sectors.jsonl"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLiveDashboardBindingsGapsAndPeriod(t *testing.T) {
	dir := liveMetricsFile(t)
	view, err := RenderDashboard([]byte(liveDashboardJSON), dir, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	p := view.Panels[0]
	if p.DataUpdatedAt == "" || p.Points != 3 || p.Missing != 1 || p.Width != 12 || p.LatestObservation != "2026-10-15" {
		t.Fatalf("panel = %+v", p)
	}
	if len(p.Series) != 1 || p.Series[0]["frequency"] != "weekly" || p.Series[0]["revision"] != "second" {
		t.Fatalf("metadata = %+v", p.Series)
	}
	var option map[string]any
	if err := json.Unmarshal(p.ECharts, &option); err != nil {
		t.Fatal(err)
	}
	axis := option["xAxis"].(map[string]any)["data"].([]any)
	if axis[0] != "2026-10-01" || axis[2] != "2026-10-15" {
		t.Fatalf("date-only precision lost: %v", axis)
	}
	series := option["series"].([]any)
	if len(series) != 1 {
		t.Fatalf("series = %v", series)
	}
	chart := string(p.ECharts)
	if !strings.Contains(chart, "null") || strings.Contains(chart, "99") || !strings.Contains(chart, "not published") {
		t.Fatalf("missing gap or entity leaked: %s", chart)
	}
	since, _ := ParseTime("2026-10-09")
	view, err = RenderDashboard([]byte(liveDashboardJSON), dir, since)
	if err != nil || view.Panels[0].Points != 1 {
		t.Fatalf("period = %+v, %v", view, err)
	}
	since, _ = ParseTime("2027-01-01")
	view, err = RenderDashboard([]byte(liveDashboardJSON), dir, since)
	if err != nil || view.Panels[0].Points != 0 || view.Panels[0].LatestObservation != "2026-10-15" {
		t.Fatalf("empty period = %+v, %v", view, err)
	}
	// Same saved definition must resolve updated observations, without regeneration.
	f, _ := os.OpenFile(filepath.Join(dir, "sectors.jsonl"), os.O_APPEND|os.O_WRONLY, 0600)
	_, _ = f.WriteString("{\"name\":\"level\",\"entity\":\"A\",\"time\":\"2027-01-02\",\"value\":0}\n")
	f.Close()
	view, err = RenderDashboard([]byte(liveDashboardJSON), dir, since)
	if err != nil || view.Panels[0].Points != 1 {
		t.Fatalf("refresh = %+v, %v", view, err)
	}
}

func TestDashboardSelectedEntitiesAreNotDuplicatedOrLeaked(t *testing.T) {
	dir := liveMetricsFile(t)
	raw := strings.Replace(liveDashboardJSON, `level{entity=\"A\"}`, `level`, 1)
	md, _, err := ResolveDashboard([]byte(raw), dir)
	if err != nil {
		t.Fatal(err)
	}
	spec := fenceJSON.FindStringSubmatch(md)[1]
	var decoded struct {
		Encoding struct {
			Y []any `json:"y"`
		} `json:"encoding"`
	}
	if err := json.Unmarshal([]byte(spec), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Encoding.Y) != 1 {
		t.Fatalf("color split creates duplicate channels: %s", spec)
	}
}

func TestLiveDashboardBoundary(t *testing.T) {
	dir := liveMetricsFile(t)
	outside := filepath.Join(t.TempDir(), "outside.jsonl")
	os.WriteFile(outside, []byte("{}\n"), 0600)
	if err := os.Symlink(outside, filepath.Join(dir, "escape.jsonl")); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		strings.Replace(liveDashboardJSON, "sectors.jsonl", "../sectors.jsonl", 1),
		strings.Replace(liveDashboardJSON, "sectors.jsonl", "escape.jsonl", 1),
		strings.Replace(liveDashboardJSON, "timeseries", "text", 1),
		strings.Replace(liveDashboardJSON, `"w":12`, `"w":25`, 1),
		`{"title":"x","panels":[]}`,
		strings.Replace(liveDashboardJSON, `"title":"Sectors"`, `"title":"Sectors","templating":{"list":[{}]}`, 1),
	} {
		if _, err := RenderDashboard([]byte(raw), dir, time.Time{}); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestLiveDashboardFoldedLabelsAndNestedProvenance(t *testing.T) {
	dir := t.TempDir()
	body := `{"name":"level{provider=\"demo\",series=\"a\",unit=\"percent\"}","entity":"A","time":"2026-10-01T02:00:00+02:00","value":1,"unit":"percent","frequency":"weekly","scope":"sector","href":"https://example.test/1","provenance":[{"source":{"published_at":"2026-10-01T03:00:00Z","source_version":"release-1"}}]}
{"name":"level{provider=\"demo\",series=\"b\",unit=\"percent\"}","entity":"B","time":"2026-10-01T00:30:00Z","value":2,"unit":"percent","frequency":"weekly","scope":"sector"}
`
	file := filepath.Join(dir, "sectors.jsonl")
	os.WriteFile(file, []byte(body), 0600)
	raw := strings.Replace(liveDashboardJSON, `level{entity=\"A\"}`, `level`, 1)
	view, err := RenderDashboard([]byte(raw), dir, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	panel := view.Panels[0]
	if panel.Points != 2 || len(panel.Series) != 2 || !strings.Contains(panel.Series[0]["provenance"], "release-1") || !strings.Contains(string(panel.ECharts), "https://example.test/1") {
		t.Fatalf("lost metadata/series: %+v", panel)
	}
	if panel.LatestObservation != "2026-10-01T00:30:00Z" {
		t.Fatalf("latest not chronological: %s", panel.LatestObservation)
	}
	os.WriteFile(file, []byte(strings.Replace(body, `"unit":"percent","frequency":"weekly","scope":"sector"}`, `"unit":"count","frequency":"weekly","scope":"sector"}`, 1)), 0600)
	if _, err := RenderDashboard([]byte(raw), dir, time.Time{}); err == nil {
		t.Fatal("mixed units accepted")
	}
	os.WriteFile(file, []byte(body+"{\"name\":\"level\",\"time\":\"2026-10-08\",\"value\":null}\n"), 0600)
	if _, err := RenderDashboard([]byte(raw), dir, time.Time{}); err == nil {
		t.Fatal("invalid null silently discarded")
	}
}

func TestLiveDashboardOrdersAcrossSeriesAndTimezones(t *testing.T) {
	dir := t.TempDir()
	rows := `{"name":"level","entity":"A","time":"2026-10-01T13:00:00+09:00","value":2}
{"name":"level","entity":"A","time":"2026-10-01T05:00:00Z","value":1}
{"name":"level","entity":"A","time":"2026-10-01","value":3}
`
	if err := os.WriteFile(filepath.Join(dir, "sectors.jsonl"), []byte(rows), 0600); err != nil {
		t.Fatal(err)
	}
	raw := strings.Replace(liveDashboardJSON, `level{entity=\"A\"}`, `level`, 1)
	view, err := RenderDashboard([]byte(raw), dir, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var option struct {
		XAxis struct {
			Data []string `json:"data"`
		} `json:"xAxis"`
	}
	if err := json.Unmarshal(view.Panels[0].ECharts, &option); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(view.Panels[0].ECharts), "2026-10-01T13:00:00+09:00") {
		t.Fatal("original offset timestamp missing from tooltip")
	}
	want := []string{"2026-10-01T00:00:00.000000000Z", "2026-10-01T04:00:00.000000000Z", "2026-10-01T05:00:00.000000000Z"}
	if len(option.XAxis.Data) != len(want) {
		t.Fatalf("axis = %v", option.XAxis.Data)
	}
	for i, label := range want {
		if option.XAxis.Data[i] != label {
			t.Fatalf("axis = %v", option.XAxis.Data)
		}
	}
}

func TestLiveDashboardInfersUnitFromRecords(t *testing.T) {
	raw := strings.Replace(liveDashboardJSON, `"unit":"pct"`, `"unit":"none"`, 1)
	view, err := RenderDashboard([]byte(raw), liveMetricsFile(t), time.Time{})
	if err != nil || view.Panels[0].Unit != "pct" {
		t.Fatalf("unit: %+v %v", view, err)
	}
}

func TestDashboardRejectsUnmatchedForeignDatasource(t *testing.T) {
	dir := t.TempDir()
	a := `{"name":"m","time":"2026-10-01","value":1,"unit":"count"}
{"name":"n","time":"2026-10-01","value":999,"unit":"count"}
`
	b := `{"name":"other","time":"2026-10-01","value":2,"unit":"count"}
`
	for file, body := range map[string]string{"a.jsonl": a, "b.jsonl": b} {
		if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	raw := `{"title":"Bindings","panels":[{"title":"Samples","type":"timeseries","datasource":{"type":"track","uid":"a.jsonl"},"targets":[{"expr":"m"},{"expr":"n","datasource":{"type":"track","uid":"b.jsonl"}}]}]}`
	if _, err := RenderDashboard([]byte(raw), dir, time.Time{}); err == nil {
		t.Fatal("unmatched foreign target read the other source without a binding error")
	}
	if _, _, err := ResolveDashboard([]byte(raw), dir); err == nil {
		t.Fatal("importer accepted mixed bindings when one target has no matching observations")
	}
	sameFile := strings.ReplaceAll(raw, "b.jsonl", "a.jsonl")
	view, err := RenderDashboard([]byte(sameFile), dir, time.Time{})
	if err != nil || len(view.Panels) != 1 || view.Panels[0].Points != 2 {
		t.Fatalf("same-file bindings: %d panels, error %v", len(view.Panels), err)
	}
}

package metrics

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDashboardSelectionAndPanelTypes(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "cpu.jsonl"),
		`{"version":1,"name":"cpu","entity":"a","time":"2026-09-18T23:30:00Z","value":60}`,
		`{"version":1,"name":"cpu","entity":"a","time":"2026-09-19T00:00:00+09:00","value":30}`,
		`{"version":1,"name":"cpu","entity":"a","time":"2026-09-19T21:00:00Z","value":85}`,
		`{"version":1,"name":"cpu","entity":"b","time":"2026-09-19T22:00:00Z","value":45}`,
		`{"version":1,"name":"latency","entity":"a","time":"2026-09-19T22:00:00Z","value":900}`)
	spec := []byte(`{"title":"Services","panels":[
  {"type":"stat","title":"CPU","gridPos":{"x":0,"y":0,"w":8,"h":4},"datasource":{"type":"track","uid":"cpu.jsonl"},"targets":[{"expr":"cpu"}],"fieldConfig":{"defaults":{"unit":"percent","decimals":1,"thresholds":{"steps":[{"value":null},{"value":80},{"value":50}]}}}},
  {"type":"timeseries","title":"CPU history","gridPos":{"x":8,"y":0,"w":16,"h":8},"datasource":{"type":"track","uid":"cpu.jsonl"},"targets":[{"expr":"cpu{entity=\"a\"}","legendFormat":"{{entity}}"}],"fieldConfig":{"defaults":{"min":0,"max":100}}},
  {"type":"table","title":"Latest CPU","datasource":{"type":"track","uid":"cpu.jsonl"},"targets":[{"expr":"cpu"}]}]}`)
	got, err := ResolveDashboardView(spec, dir, DashboardSelection{From: "2026-09-19", To: "2026-09-19", Entity: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Services" || strings.Join(got.Entities, ",") != "a,b" || got.Asof != "2026-09-19T21:00:00Z" {
		t.Fatalf("bad scope: %+v", got)
	}
	v := got.Panels[0].Values
	if len(v) != 1 || v[0].Value != 85 || v[0].Display != "85.0 %" || v[0].Threshold == nil || *v[0].Threshold != 80 {
		t.Fatalf("bad latest: %+v", v)
	}
	if got.Panels[0].Grid.W != 8 || got.Panels[1].Grid.X != 8 || got.Panels[2].Grid.Y != 8 {
		t.Fatalf("grid lost: %+v", got.Panels)
	}
	if len(got.Panels[2].Values) != 1 || len(got.Panels[2].ECharts) != 0 {
		t.Fatalf("bad table: %+v", got.Panels[2])
	}
	var chart struct {
		Series []struct {
			Name string `json:"name"`
			Data []any  `json:"data"`
		} `json:"series"`
		YAxis []struct {
			Min float64 `json:"min"`
			Max float64 `json:"max"`
		} `json:"yAxis"`
	}
	if err := json.Unmarshal(got.Panels[1].ECharts, &chart); err != nil {
		t.Fatal(err)
	}
	if len(chart.Series) != 1 || chart.Series[0].Name != "a" || len(chart.Series[0].Data) != 1 || len(chart.YAxis) != 1 || chart.YAxis[0].Max != 100 {
		t.Fatalf("chart selection/axes lost: %s", got.Panels[1].ECharts)
	}
	// With no selected target, a panel's own matcher still excludes b and the other metric.
	all, err := ResolveDashboardView(spec, dir, DashboardSelection{})
	if err != nil || len(all.Panels[0].Values) != 2 || len(all.Panels[1].Values) != 1 {
		t.Fatalf("matcher leaked: %v %+v", err, all)
	}
	// Mobile and assistive reading order follows grid positions, not JSON order.
	var shuffled grafanaDashboard
	if err := json.Unmarshal(spec, &shuffled); err != nil {
		t.Fatal(err)
	}
	shuffled.Panels[0], shuffled.Panels[1] = shuffled.Panels[1], shuffled.Panels[0]
	shuffledSpec, _ := json.Marshal(shuffled)
	ordered, err := ResolveDashboardView(shuffledSpec, dir, DashboardSelection{})
	if err != nil || ordered.Panels[0].Title != "CPU" || ordered.Panels[1].Title != "CPU history" {
		t.Fatalf("grid reading order lost: %v %+v", err, ordered)
	}
	// Chronology uses instants, not string order across UTC offsets.
	earlier, err := ResolveDashboardView(spec, dir, DashboardSelection{To: "2026-09-18", Entity: "a"})
	if err != nil || earlier.Panels[0].Values[0].Value != 60 {
		t.Fatalf("wrong chronological latest: %v %+v", err, earlier)
	}
	empty, err := ResolveDashboardView(spec, dir, DashboardSelection{From: "2027-01-01"})
	if err != nil || len(empty.Panels[0].Values) != 0 || empty.Asof != "" || len(empty.Panels[1].ECharts) != 0 || len(empty.Entities) != 2 {
		t.Fatalf("bad empty selection: %v %+v", err, empty)
	}
	if _, err := ResolveDashboardView(spec, dir, DashboardSelection{From: "2026-09-20", To: "2026-09-19"}); err == nil {
		t.Fatal("reversed period accepted")
	}
	if _, err := ResolveDashboardView(spec, dir, DashboardSelection{From: "yesterday"}); err == nil {
		t.Fatal("invalid period accepted")
	}
	md, n, err := ResolveDashboard(spec, dir)
	if err != nil || n != 3 || strings.Count(md, "```metrics-dashboard") != 1 || !strings.Contains(md, `"gridPos"`) {
		t.Fatalf("conversion flattened dashboard: %v %s", err, md)
	}
}

func TestDashboardDataFailuresAndBoundaries(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "ok.jsonl"), `{"name":"v","time":"2026-09-19","value":7}`)
	write(t, filepath.Join(dir, "invalid.jsonl"), `{"name":"v","time":"yesterday","value":7}`)
	write(t, filepath.Join(dir, "duplicate.jsonl"), `{"name":"v","time":"2026-09-19","value":7}`, `{"name":"v","time":"2026-09-19T00:00:00Z","value":8}`)
	write(t, filepath.Join(dir, "nonfinite.jsonl"), `{"name":"v","time":"2026-09-19","value":"NaN"}`)
	outside := filepath.Join(t.TempDir(), "secret.jsonl")
	write(t, outside, `{"name":"v","time":"2026-09-19","value":42}`)
	if err := os.Symlink(outside, filepath.Join(dir, "escape.jsonl")); err != nil {
		t.Fatal(err)
	}
	makeSpec := func(file string) []byte {
		b, _ := json.Marshal(map[string]any{"title": "Test", "panels": []any{
			map[string]any{"type": "stat", "title": "Broken", "datasource": GrafanaDatasource{Type: "track", UID: file}, "targets": []any{map[string]string{"expr": "v"}}},
			map[string]any{"type": "stat", "title": "Healthy", "datasource": GrafanaDatasource{Type: "track", UID: "ok.jsonl"}, "targets": []any{map[string]string{"expr": "v"}}},
		}})
		return b
	}
	for _, file := range []string{"missing.jsonl", "invalid.jsonl", "duplicate.jsonl", "escape.jsonl", "nonfinite.jsonl"} {
		t.Run(file, func(t *testing.T) {
			result, err := ResolveDashboardView(makeSpec(file), dir, DashboardSelection{})
			if err != nil || result.Panels[0].Error == "" || len(result.Panels[0].Values) != 0 || len(result.Panels[1].Values) != 1 {
				t.Fatalf("expected isolated error: %v %+v", err, result)
			}
			if _, _, err := ResolveDashboard(makeSpec(file), dir); err == nil {
				t.Fatal("CLI must reject invalid data")
			}
		})
	}
	for _, file := range []string{"../secret.jsonl", outside, "..", `a\b.jsonl`} {
		if _, err := ResolveDashboardView(makeSpec(file), dir, DashboardSelection{}); err == nil {
			t.Fatalf("unsafe datasource accepted: %q", file)
		}
	}
	for _, replacement := range []string{`"type":"gauge"`, `"type":"stat","gridPos":{"x":9223372036854775807,"y":0,"w":1,"h":4}`, `"type":"stat","gridPos":{"x":23,"y":0,"w":2,"h":4}`, `"type":"stat","transformations":[{}]`, `"type":"stat","options":{"reduceOptions":{"calcs":["sum"]}}`, `"type":"stat","fieldConfig":{"defaults":{"thresholds":{"mode":"percentage"}}}`} {
		bad := strings.Replace(string(makeSpec("ok.jsonl")), `"type":"stat"`, replacement, 1)
		if _, err := ResolveDashboardView([]byte(bad), dir, DashboardSelection{}); err == nil {
			t.Fatalf("unsupported configuration accepted: %s", bad)
		}
	}
}

func TestDashboardMetricFilterAndUnits(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "m.jsonl"), `{"name":"cpu","entity":"web1","time":"2026-09-19","value":0.85}`, `{"name":"memory","entity":"web1","time":"2026-09-19","value":0.4}`)
	spec := []byte(`{"title":"Usage","panels":[{"type":"timeseries","datasource":{"type":"track","uid":"m.jsonl"},"targets":[{"expr":"cpu","legendFormat":"usage"},{"expr":"memory","legendFormat":"usage"}],"fieldConfig":{"defaults":{"unit":"percentunit","min":0,"max":1,"thresholds":{"steps":[{"value":0.8}]}}}}]}`)
	result, err := ResolveDashboardView(spec, dir, DashboardSelection{Metric: "cpu"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(result.Metrics, ",") != "cpu,memory" || len(result.Panels[0].Values) != 1 {
		t.Fatalf("metric selector lost scope: %+v", result)
	}
	value := result.Panels[0].Values[0]
	if value.Display != "85.00 %" || value.ThresholdDisplay != "80.00 %" {
		t.Fatalf("units disagree: %+v", value)
	}
	var option struct {
		Series []struct {
			Data []float64 `json:"data"`
		} `json:"series"`
		YAxis []struct {
			Max float64 `json:"max"`
		} `json:"yAxis"`
	}
	if err := json.Unmarshal(result.Panels[0].ECharts, &option); err != nil {
		t.Fatal(err)
	}
	if len(option.Series) != 1 || option.Series[0].Data[0] != 85 || option.YAxis[0].Max != 100 {
		t.Fatalf("chart unit mismatch: %s", result.Panels[0].ECharts)
	}
	all, err := ResolveDashboardView(spec, dir, DashboardSelection{})
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(all.Panels[0].ECharts, &option); err != nil || len(option.Series) != 2 {
		t.Fatalf("same legends merged series: %v %s", err, all.Panels[0].ECharts)
	}
}

package render

import (
	"strings"
	"testing"
)

func TestMissingMetricRenderBoundary(t *testing.T) {
	for _, mark := range []string{"line", "area", "bar"} {
		t.Run(mark, func(t *testing.T) {
			spec := `{"version":2,"mark":"` + mark + `","data":{"kind":"metric","records":[
{"name":"demo","time":"d1","value":1},
{"name":"demo","time":"d2","value":2},
{"name":"demo","time":"d3","value":null,"missing_reason":"not published","url":"https://example.com/evidence"},
{"name":"demo","time":"d4","value":0},
{"name":"demo","time":"d5","value":-1}]},
"encoding":{"x":{"field":"time"},"y":[{"field":"value"}]}}`
			res, err := resolveSpecDir([]byte(spec), "")
			if err != nil {
				t.Fatal(err)
			}
			option := echartsOptionForTest(t, res)
			series := option["series"].([]any)[0].(map[string]any)
			values := series["data"].([]any)
			if len(values) != 5 || values[2] != nil || values[3] != float64(0) {
				t.Fatalf("data = %v", values)
			}
			if mark != "bar" && series["connectNulls"] != false {
				t.Fatalf("gap interpolation enabled: %v", series)
			}
			svg, err := (SVG{}).Render(res)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(svg, "NaN") || strings.Contains(svg, "Inf") {
				t.Fatal("invalid SVG geometry")
			}
			if mark != "bar" && strings.Count(svg, "<polyline ") != 2 {
				t.Fatalf("expected separate line runs: %s", svg)
			}
			if mark == "area" && strings.Count(svg, "<polygon ") != 2 {
				t.Fatalf("expected separate area runs: %s", svg)
			}
			bad := strings.Replace(spec, `"missing_reason":"not published",`, "", 1)
			if _, err := EChartsOptionFromSpecDir([]byte(bad), ""); err == nil {
				t.Fatal("unexplained null rendered")
			}
		})
	}
}

func TestMissingMetricRenderKeepsDetails(t *testing.T) {
	spec := `{"version":2,"mark":"line","data":{"kind":"metric","records":[{"name":"demo","time":"d1","value":null,"missing_reason":"not published","url":"https://example.com/evidence"}]},"encoding":{"x":{"field":"time"},"y":[{"field":"value"}],"detail":[{"field":"missing_reason","title":"Missing"}],"href":{"field":"url"}}}`
	out, err := EChartsOptionFromSpecDir([]byte(spec), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"value":null`, `not published`, `https://example.com/evidence`} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %s: %s", want, out)
		}
	}
}

func TestMissingMetricGaugeRendererClearsLatest(t *testing.T) {
	spec := `{"version":2,"mark":"gauge","data":{"kind":"metric","records":[{"name":"demo","time":"d1","value":37},{"name":"demo","time":"d2","value":null,"missing_reason":"not published"}]},"encoding":{"y":[{"field":"value"}]}}`
	res, err := resolveSpecDir([]byte(spec), "")
	if err != nil {
		t.Fatal(err)
	}
	opt := echartsOptionForTest(t, res)
	series := opt["series"].([]any)[0].(map[string]any)
	if got := series["data"].([]any)[0].(map[string]any)["value"]; got != nil {
		t.Fatalf("stale gauge value=%v", got)
	}
	if series["pointer"].(map[string]any)["show"] != false || series["detail"].(map[string]any)["show"] != false {
		t.Fatal("missing gauge must not point at its minimum or label NaN")
	}
	svg, err := (SVG{}).Render(res)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(svg, `stroke-width="4"`) || strings.Contains(svg, `font-size="36"`) {
		t.Fatalf("missing gauge drew a reading/needle: %s", svg)
	}
	res.Gauge.Value = 0 // a recovered real zero must be visible again
	series = echartsOptionForTest(t, res)["series"].([]any)[0].(map[string]any)
	if series["pointer"].(map[string]any)["show"] != true || series["detail"].(map[string]any)["show"] != true {
		t.Fatal("observed zero did not restore gauge")
	}
}

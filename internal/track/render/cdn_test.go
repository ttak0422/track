package render

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// The standalone HTML artifact loads ECharts from a CDN while the web frontend bundles it
// (ADR 0029); both must draw a chart option with the same ECharts version. Guard the pin: the
// version in echartsCDN must equal the one web/package.json declares.
func TestEChartsCDNMatchesWebBundle(t *testing.T) {
	raw, err := os.ReadFile("../../../web/package.json")
	if err != nil {
		t.Fatalf("read web/package.json: %v", err)
	}
	var pkg struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal(raw, &pkg); err != nil {
		t.Fatalf("parse web/package.json: %v", err)
	}
	webVersion := strings.TrimLeft(pkg.Dependencies["echarts"], "^~")
	if webVersion == "" {
		t.Fatal("web/package.json declares no echarts dependency")
	}
	if want := "https://cdn.jsdelivr.net/npm/echarts@" + webVersion; echartsCDN != want {
		t.Errorf("echartsCDN = %q, want %q (pin to the version web/package.json bundles)", echartsCDN, want)
	}
}

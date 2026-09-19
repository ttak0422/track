package site

import (
	"encoding/json"

	"github.com/ttak0422/track/internal/track/babel"
	"github.com/ttak0422/track/internal/track/metrics"
)

// Resolve only dashboards in published bodies. The snapshot carries visible panel values and
// chart options, not the source JSONL files; it uses the same resolver as the live readers.
func resolveMetricsDashboardBlocks(body, dataDir string) string {
	return babel.ReplaceBlocks(body, "metrics-dashboard", func(b babel.Block) []string {
		result, err := metrics.ResolveDashboardView([]byte(b.Body), dataDir, metrics.DashboardSelection{})
		if err != nil {
			return []string{"> Metrics dashboard error: " + err.Error(), "", "```json", b.Body, "```"}
		}
		data, err := json.Marshal(result)
		if err != nil {
			return []string{"> Metrics dashboard error: " + err.Error(), "", "```json", b.Body, "```"}
		}
		return []string{"```metrics-dashboard-snapshot", string(data), "```"}
	})
}

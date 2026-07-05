package site

import (
	"strings"

	"github.com/ttak0422/track/internal/track/babel"
	"github.com/ttak0422/track/internal/track/render"
)

// viewSpecLang is the fence language that marks an embedded View Spec chart (```viewspec ... ```),
// mirroring how ```mermaid marks an embedded diagram.
const viewSpecLang = "viewspec"

// echartsLang is the fence language the export emits for a resolved chart: the body is a ready-to-draw
// ECharts option (pure JSON), which the frontend hands to a bundled ECharts instance. Resolution
// (data reading, filtering, encoding) happens here at build time, so the published site needs no
// chart engine on the server and no vault data — only the drawing library it already bundles.
const echartsLang = "echarts"

// resolveViewSpecBlocks replaces every fenced ```viewspec block in a published body with a fenced
// ```echarts block carrying the spec's resolved ECharts option, so the static site draws the same
// interactive chart as the live workspace. dataDir resolves data.source references at build time.
//
// A spec that fails to load or resolve is replaced by an inline error plus the original JSON as a code
// block — the page still publishes, matching how the live workspace shows a bad spec at the block
// position.
func resolveViewSpecBlocks(body, dataDir string) string {
	lines := strings.Split(body, "\n")
	var out []string
	next := 0
	for _, b := range babel.ParseBlocks(body) {
		if !strings.EqualFold(b.Language, viewSpecLang) {
			continue
		}
		out = append(out, lines[next:b.StartLine]...)
		opt, err := render.EChartsOptionFromSpecDir([]byte(b.Body), dataDir)
		if err != nil {
			out = append(out, "> View Spec error: "+err.Error(), "", "```json", b.Body, "```")
		} else {
			out = append(out, "```"+echartsLang, opt, "```")
		}
		next = b.EndLine + 1
	}
	if next == 0 {
		return body // no viewspec fences: the common case, untouched
	}
	out = append(out, lines[next:]...)
	return strings.Join(out, "\n")
}

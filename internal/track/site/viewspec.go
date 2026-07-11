package site

import (
	"encoding/json"
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
// noteSlug maps a vault note id (a chart datum's "note" provenance) to its published slug; references
// it cannot map are dropped so the published chart never navigates to a hidden internal id.
//
// A spec that fails to load or resolve is replaced by an inline error plus the original JSON as a code
// block — the page still publishes, matching how the live workspace shows a bad spec at the block
// position.
func resolveViewSpecBlocks(body, dataDir string, noteSlug func(string) (string, bool)) string {
	return babel.ReplaceBlocks(body, viewSpecLang, func(b babel.Block) []string {
		opt, err := render.EChartsOptionFromSpecDir([]byte(b.Body), dataDir)
		if err != nil {
			return []string{"> View Spec error: " + err.Error(), "", "```json", b.Body, "```"}
		}
		return []string{"```" + echartsLang, rewriteNoteRefs(opt, noteSlug), "```"}
	})
}

// rewriteNoteRefs maps every "note" field in a resolved option (a chart datum's vault-note provenance)
// through noteSlug: published notes become their opaque slugs, unpublished ones are dropped. The walk
// is generic because note refs ride on data items at several depths (series data, markLine data).
func rewriteNoteRefs(optJSON string, noteSlug func(string) (string, bool)) string {
	if !strings.Contains(optJSON, `"note"`) {
		return optJSON
	}
	var opt any
	if err := json.Unmarshal([]byte(optJSON), &opt); err != nil {
		return optJSON
	}
	rewriteNoteValues(opt, noteSlug)
	out, err := json.Marshal(opt)
	if err != nil {
		return optJSON
	}
	return string(out)
}

func rewriteNoteValues(v any, noteSlug func(string) (string, bool)) {
	switch t := v.(type) {
	case map[string]any:
		if ref, ok := t["note"].(string); ok {
			if slug, found := noteSlug(ref); found {
				t["note"] = slug
			} else {
				delete(t, "note")
			}
		}
		for _, child := range t {
			rewriteNoteValues(child, noteSlug)
		}
	case []any:
		for _, child := range t {
			rewriteNoteValues(child, noteSlug)
		}
	}
}

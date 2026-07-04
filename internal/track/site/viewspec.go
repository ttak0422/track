package site

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ttak0422/track/internal/track/babel"
	"github.com/ttak0422/track/internal/track/render"
)

// viewSpecLang is the fence language that marks an embedded View Spec chart (```viewspec ... ```),
// mirroring how ```mermaid marks an embedded diagram.
const viewSpecLang = "viewspec"

// renderViewSpecBlocks replaces every fenced ```viewspec block in a published body with its rendered
// chart: the spec JSON is drawn to a static SVG (render.SVGFromSpecDir, the same engine behind
// ".viewspec.json" asset embeds) and the fence becomes an image reference to a generated asset, so the
// static site shows the chart with no client-side JavaScript. dataDir resolves data.source references.
//
// A spec that fails to load or render is replaced by an inline error plus the original JSON as a code
// block — the page still publishes, matching how the live workspace shows a bad spec at the block
// position. The returned map holds the generated SVGs by published asset name for the bundle to write.
func renderViewSpecBlocks(body, dataDir string) (string, map[string]string) {
	var charts map[string]string
	lines := strings.Split(body, "\n")
	var out []string
	next := 0
	for _, b := range babel.ParseBlocks(body) {
		if !strings.EqualFold(b.Language, viewSpecLang) {
			continue
		}
		out = append(out, lines[next:b.StartLine]...)
		svg, err := render.SVGFromSpecDir([]byte(b.Body), dataDir)
		if err != nil {
			out = append(out, "> View Spec error: "+err.Error(), "", "```json", b.Body, "```")
		} else {
			name := publishSlug("chart:"+b.BodyHash) + ".svg"
			if charts == nil {
				charts = map[string]string{}
			}
			charts[name] = svg
			out = append(out, fmt.Sprintf("![%s](assets/%s)", chartAlt(b.Body), name))
		}
		next = b.EndLine + 1
	}
	if next == 0 {
		return body, nil // no viewspec fences: the common case, untouched
	}
	out = append(out, lines[next:]...)
	return strings.Join(out, "\n"), charts
}

// chartAlt returns the image alt text for an embedded chart: the spec's title when it parses, else a
// generic label (the spec may be arbitrarily broken and still reach here on the success path only).
func chartAlt(specJSON string) string {
	var probe struct {
		Title string `json:"title"`
	}
	if json.Unmarshal([]byte(specJSON), &probe) == nil && strings.TrimSpace(probe.Title) != "" {
		// Strip the characters that would terminate the surrounding ![alt](url) syntax.
		clean := strings.Map(func(r rune) rune {
			if strings.ContainsRune("[]()\n", r) {
				return -1
			}
			return r
		}, probe.Title)
		if clean = strings.TrimSpace(clean); clean != "" {
			return clean
		}
	}
	return "Chart"
}

package webui

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// paletteVars is the whitelist of themeable CSS custom properties. Only these keys are honored in a
// palette file, so a palette can never inject arbitrary properties into the served page.
var paletteVars = map[string]bool{
	"bg":                  true,
	"panel":               true,
	"panel-soft":          true,
	"text":                true,
	"muted":               true,
	"line":                true,
	"accent":              true,
	"accent-strong":       true,
	"graph-active":        true,
	"graph-active-strong": true,
	"generated":           true,
	"danger":              true,
}

// colorValue restricts palette values to safe CSS color syntax (hex, rgb()/hsl()/keyword). It excludes
// the characters that could break out of a declaration (`;`, `{`, `}`), so values cannot inject CSS.
var colorValue = regexp.MustCompile(`^#[0-9A-Fa-f]{3,8}$|^[a-zA-Z]+$|^(rgb|rgba|hsl|hsla)\([0-9.,%/ ]+\)$`)

// paletteFile is the on-disk palette format: per-theme maps of themeable variable name to CSS color.
//
//	light:
//	  accent: "#2f6f5e"
//	  text: "#20231f"
//	dark:
//	  accent: "#62b39b"
type paletteFile struct {
	Light map[string]string `yaml:"light"`
	Dark  map[string]string `yaml:"dark"`
}

// LoadPalette reads a palette file and returns the CSS that overrides the built-in colors, scoped so it
// follows the same light/dark/system cascade as the default stylesheet. An empty path returns "" (use
// the built-in palette). Unknown keys are ignored; an invalid color value is an error so a typo is not
// silently dropped.
func LoadPalette(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read palette %s: %w", path, err)
	}
	var pf paletteFile
	if err := yaml.Unmarshal(raw, &pf); err != nil {
		return "", fmt.Errorf("parse palette %s: %w", path, err)
	}
	light, err := validatePalette(pf.Light)
	if err != nil {
		return "", fmt.Errorf("palette %s light: %w", path, err)
	}
	dark, err := validatePalette(pf.Dark)
	if err != nil {
		return "", fmt.Errorf("palette %s dark: %w", path, err)
	}
	return buildPaletteCSS(light, dark), nil
}

// validatePalette keeps only whitelisted keys and rejects malformed color values.
func validatePalette(in map[string]string) (map[string]string, error) {
	out := map[string]string{}
	for k, v := range in {
		if !paletteVars[k] {
			continue // ignore unknown keys rather than fail; the whitelist may trail the stylesheet
		}
		v = strings.TrimSpace(v)
		if !colorValue.MatchString(v) {
			return nil, fmt.Errorf("invalid color for %q: %q", k, v)
		}
		out[k] = v
	}
	return out, nil
}

// buildPaletteCSS renders override declarations matching the default stylesheet's cascade: light values
// apply to the base :root (and the light system preference), dark values apply to the dark theme and the
// dark system preference. Empty sections contribute nothing.
func buildPaletteCSS(light, dark map[string]string) string {
	if len(light) == 0 && len(dark) == 0 {
		return ""
	}
	var b strings.Builder
	if len(light) > 0 {
		b.WriteString(":root{" + declarations(light) + "}\n")
		b.WriteString("@media (prefers-color-scheme: light){:root:not([data-theme=\"dark\"]){" + declarations(light) + "}}\n")
	}
	if len(dark) > 0 {
		b.WriteString(":root[data-theme=\"dark\"]{" + declarations(dark) + "}\n")
		b.WriteString("@media (prefers-color-scheme: dark){:root:not([data-theme=\"light\"]){" + declarations(dark) + "}}\n")
	}
	return b.String()
}

// declarations renders a deterministic `--name:value;` list for one selector block.
func declarations(vars map[string]string) string {
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString("--" + k + ":" + vars[k] + ";")
	}
	return b.String()
}

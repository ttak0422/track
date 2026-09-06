package metrics

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ttak0422/track/internal/track/dataset"
)

// Supported panel types of the Grafana subset. Anything else fails loudly at parse time.
const (
	PanelTimeseries = "timeseries"
	PanelStat       = "stat"
)

// GrafanaDatasource names the track datasource convention: type "track" with the uid as a bare
// data/ filename, mirroring how viewspec data.source resolves.
type GrafanaDatasource struct {
	Type string `json:"type"`
	UID  string `json:"uid"`
}

type grafanaTarget struct {
	Expr         string             `json:"expr"`
	LegendFormat string             `json:"legendFormat"`
	Datasource   *GrafanaDatasource `json:"datasource"`
}

type grafanaThresholdStep struct {
	Value *float64 `json:"value"`
}

type grafanaThresholds struct {
	Steps []grafanaThresholdStep `json:"steps"`
}

type grafanaFieldDefaults struct {
	Unit       string             `json:"unit"`
	Min        *float64           `json:"min"`
	Max        *float64           `json:"max"`
	Thresholds *grafanaThresholds `json:"thresholds"`
}

type grafanaPanel struct {
	Type        string             `json:"type"`
	Title       string             `json:"title"`
	Targets     []grafanaTarget    `json:"targets"`
	Datasource  *GrafanaDatasource `json:"datasource"`
	FieldConfig struct {
		Defaults grafanaFieldDefaults `json:"defaults"`
	} `json:"fieldConfig"`
}

type grafanaDashboard struct {
	Title      string         `json:"title"`
	Panels     []grafanaPanel `json:"panels"`
	Templating struct {
		List []json.RawMessage `json:"list"`
	} `json:"templating"`
}

// ResolveDashboard parses a Grafana classic dashboard (subset) and emits note Markdown: one h2 per
// panel holding a viewspec fence. Timeseries panels become line charts with threshold overlays;
// stat panels resolve to the same series as a line (documented limitation — viewspec has no
// single-stat mark). gridPos is ignored (notes lay out vertically); unit/min/max are accepted and
// carried into the series title when no legendFormat is given; template variables are outside the
// subset and fail loudly.
func ResolveDashboard(data []byte, dataDir string) (string, int, error) {
	var d grafanaDashboard
	dec := json.NewDecoder(strings.NewReader(string(data)))
	if err := dec.Decode(&d); err != nil {
		return "", 0, fmt.Errorf("parse dashboard JSON: %w", err)
	}
	if len(d.Templating.List) > 0 {
		return "", 0, fmt.Errorf("templating variables are outside the subset")
	}
	if strings.TrimSpace(d.Title) == "" {
		return "", 0, fmt.Errorf("dashboard needs a title")
	}
	var b strings.Builder
	b.WriteString("# " + strings.TrimSpace(d.Title) + "\n")
	count := 0
	for i, p := range d.Panels {
		if p.Type != PanelTimeseries && p.Type != PanelStat {
			return "", 0, fmt.Errorf("panel %d %q: type %q is outside the subset (want timeseries|stat)", i, p.Title, p.Type)
		}
		if len(p.Targets) == 0 {
			return "", 0, fmt.Errorf("panel %d %q: no targets", i, p.Title)
		}
		fence, err := resolvePanel(p, dataDir)
		if err != nil {
			return "", 0, fmt.Errorf("panel %d %q: %w", i, p.Title, err)
		}
		title := strings.TrimSpace(p.Title)
		if title == "" {
			title = fmt.Sprintf("panel %d", i+1)
		}
		b.WriteString("\n\n## " + title + "\n\n```viewspec\n" + fence + "\n```\n")
		count++
	}
	return b.String(), count, nil
}

func resolvePanel(p grafanaPanel, dataDir string) (string, error) {
	type ySeries struct {
		Field string `json:"field"`
		Title string `json:"title,omitempty"`
		Mark  string `json:"mark,omitempty"`
	}
	type overlay struct {
		Y     *float64 `json:"y,omitempty"`
		Label string   `json:"label,omitempty"`
	}
	type filterCond struct {
		Field string `json:"field"`
		Op    string `json:"op"`
		Value string `json:"value"`
	}
	for _, t := range p.Targets {
		if _, err := ParseQuery(t.Expr); err != nil {
			return "", fmt.Errorf("target %q: %w (expr is a metric name plus = matchers only)", t.Expr, err)
		}
		ds := t.Datasource
		if ds == nil {
			ds = p.Datasource
		}
		if ds == nil || ds.Type != "track" || !isBareFilename(ds.UID) {
			got := "absent"
			if ds != nil {
				got = fmt.Sprintf("%q/%q", ds.Type, ds.UID)
			}
			return "", fmt.Errorf("target %q: datasource must be {\"type\":\"track\",\"uid\":\"<data/ filename>\"}, got %s", t.Expr, got)
		}
		if _, err := loadMetricFile(dataDir, ds.UID); err != nil {
			return "", err
		}
	}
	// Re-resolve per target to build series titles: legendFormat with {{label}} substitution
	// against each matched series' labels, else entity + family or the series name.
	type series struct {
		file   string
		name   string
		entity string
		title  string
	}
	var seriesList []series
	seriesSeen := map[string]bool{}
	for _, t := range p.Targets {
		q, _ := ParseQuery(t.Expr)
		ds := t.Datasource
		if ds == nil {
			ds = p.Datasource
		}
		rows, _ := loadMetricFile(dataDir, ds.UID)
		for _, r := range rows {
			nm, _ := r.String("name")
			ent, _ := r.String("entity")
			if !MatchRecordWithEntity(nm, ent, q) {
				continue
			}
			_, labels, _ := SplitFolded(nm)
			title := strings.TrimSpace(t.LegendFormat)
			if title == "" {
				if ent != "" {
					title = ent + " " + q.Family
				} else {
					title = nm
				}
			} else {
				for k, v := range labels {
					title = strings.ReplaceAll(title, "{{"+k+"}}", v)
				}
				title = strings.ReplaceAll(title, "{{entity}}", ent)
			}
			if p.FieldConfig.Defaults.Unit != "" && strings.TrimSpace(t.LegendFormat) == "" {
				title += " (" + p.FieldConfig.Defaults.Unit + ")"
			}
			key := ds.UID + "\x00" + nm + "\x00" + ent + "\x00" + title
			if !seriesSeen[key] {
				seriesSeen[key] = true
				seriesList = append(seriesList, series{file: ds.UID, name: nm, entity: ent, title: title})
			}
		}
	}
	if len(seriesList) == 0 {
		return "", seriesListErr(p)
	}
	files := map[string]bool{}
	folded := map[string]bool{}
	entities := map[string]bool{}
	for _, s := range seriesList {
		files[s.file] = true
		folded[s.name] = true
		entities[s.entity] = true
	}
	if len(files) > 1 {
		return "", fmt.Errorf("one spec reads one file: panel %q targets span %d files", p.Title, len(files))
	}
	if len(folded) > 1 {
		return "", fmt.Errorf("panel %q matches %d series names; split the targets across panels", p.Title, len(folded))
	}
	file := seriesList[0].file
	family := seriesList[0].name
	multi := len(entities) > 1
	ys := make([]ySeries, len(seriesList))
	for i, s := range seriesList {
		ys[i] = ySeries{Field: "value", Title: s.title}
		if multi && i > 0 {
			// Under a color split y[0] shares the chart's mark while every extra
			// channel is an explicit series that needs its own mark override.
			ys[i].Mark = "line"
		}
	}
	spec := map[string]any{
		"version": 2,
		"title":   strings.TrimSpace(p.Title),
		"mark":    "line",
		"data":    map[string]any{"source": file, "kind": "metric"},
		"filter": map[string]any{"all": []filterCond{
			{Field: "name", Op: "eq", Value: family},
		}},
		"encoding": map[string]any{
			"x": map[string]any{"field": "time"},
			"y": ys,
		},
	}
	if multi {
		enc := spec["encoding"].(map[string]any)
		enc["color"] = map[string]any{"field": "entity", "type": "nominal"}
	}
	if p.FieldConfig.Defaults.Thresholds != nil {
		var ovs []overlay
		for _, st := range p.FieldConfig.Defaults.Thresholds.Steps {
			if st.Value == nil {
				continue // the base step carries no value
			}
			v := *st.Value
			ovs = append(ovs, overlay{Y: &v, Label: fmt.Sprintf("threshold %s", trimFloat(v))})
		}
		if len(ovs) > 0 {
			spec["overlays"] = ovs
		}
	}
	enc, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return "", err
	}
	return string(enc), nil
}

func seriesListErr(p grafanaPanel) error {
	return fmt.Errorf("panel %q resolved no series", p.Title)
}

func trimFloat(v float64) string {
	s := fmt.Sprintf("%v", v)
	return s
}

func isBareFilename(s string) bool {
	return s != "" && s == filepath.Base(s) && !strings.Contains(s, `\`)
}

func loadMetricFile(dataDir, file string) ([]dataset.Record, error) {
	f, err := os.Open(filepath.Join(dataDir, file))
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", file, err)
	}
	defer f.Close()
	rows, err := dataset.ReadJSONL(f)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", file, err)
	}
	// Keep only metric-shaped rows; other kinds never enter a panel query.
	var out []dataset.Record
	for _, r := range rows {
		if _, ok := r.String("name"); !ok {
			continue
		}
		if _, ok := r.Float("value"); !ok {
			continue
		}
		if _, ok := r.String("time"); !ok {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		ti, _ := out[i].String("time")
		tj, _ := out[j].String("time")
		return ti < tj
	})
	return out, nil
}

package metrics

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/track/dataset"
	"github.com/ttak0422/track/internal/track/render"
	"github.com/ttak0422/track/internal/track/viewspec"
)

// DashboardView is a resolved, read-only Grafana-subset dashboard. Definitions stay in notes;
// observations stay in data/. No endpoint fetches outside the vault or computes domain metrics.
type DashboardView struct {
	Title  string           `json:"title"`
	Panels []DashboardPanel `json:"panels"`
}

type MissingObservation struct {
	Time   string `json:"time"`
	Entity string `json:"entity"`
	Reason string `json:"reason"`
}

type DashboardPanel struct {
	Gaps              []MissingObservation `json:"gaps"`
	Series            []map[string]string  `json:"series"`
	Title             string               `json:"title"`
	Unit              string               `json:"unit,omitempty"`
	X                 int                  `json:"x"`
	Width             int                  `json:"width"`
	Source            string               `json:"source"`
	LatestObservation string               `json:"latest_observation,omitempty"`
	DataUpdatedAt     string               `json:"data_updated_at,omitempty"`
	Points            int                  `json:"points"`
	Missing           int                  `json:"missing"`
	ECharts           json.RawMessage      `json:"echarts"`
}

// RenderDashboard reuses the importer's validation and chart semantics, but binds records afresh on
// every request. since is an observation-time cutoff, never an instruction to interpolate or fetch.
func RenderDashboard(raw []byte, dataDir string, since time.Time) (DashboardView, error) {
	var d grafanaDashboard
	if err := json.Unmarshal(raw, &d); err != nil {
		return DashboardView{}, fmt.Errorf("parse dashboard JSON: %w", err)
	}
	if strings.TrimSpace(d.Title) == "" {
		return DashboardView{}, fmt.Errorf("dashboard needs a title")
	}
	if len(d.Templating.List) != 0 {
		return DashboardView{}, fmt.Errorf("templating variables are outside the subset")
	}
	if len(d.Panels) == 0 || len(d.Panels) > 48 {
		return DashboardView{}, fmt.Errorf("dashboard needs 1–48 panels")
	}
	sort.SliceStable(d.Panels, func(i, j int) bool {
		a, b := d.Panels[i].GridPos, d.Panels[j].GridPos
		if a == nil {
			a = &grafanaGridPos{}
		}
		if b == nil {
			b = &grafanaGridPos{}
		}
		if a.Y != b.Y {
			return a.Y < b.Y
		}
		return a.X < b.X
	})
	result := DashboardView{Title: d.Title, Panels: []DashboardPanel{}}
	for i, p := range d.Panels {
		if p.Type != PanelTimeseries && p.Type != PanelStat {
			return DashboardView{}, fmt.Errorf("panel %d: type %q is outside the subset", i, p.Type)
		}
		if len(p.Targets) == 0 {
			return DashboardView{}, fmt.Errorf("panel %d: no targets", i)
		}
		specJSON, err := resolvePanelSpec(p, dataDir, true)
		if err != nil {
			return DashboardView{}, fmt.Errorf("panel %d: %w", i, err)
		}
		var spec viewspec.Spec
		if err := json.Unmarshal([]byte(specJSON), &spec); err != nil {
			return DashboardView{}, err
		}
		file := spec.Data.Source
		rows, info, err := readMetricRecords(dataDir, file)
		if err != nil {
			return DashboardView{}, err
		}
		if err := dataset.ValidateRecords(dataset.KindMetric, rows); err != nil {
			return DashboardView{}, err
		}
		panel := DashboardPanel{Title: p.Title, Unit: p.FieldConfig.Defaults.Unit, Width: 24, Source: file}
		if p.GridPos != nil {
			g := p.GridPos
			if g.X < 0 || g.X > 23 || g.W < 1 || g.W > 24 || g.X+g.W > 24 || g.Y < 0 {
				return DashboardView{}, fmt.Errorf("panel %d: gridPos requires x >= 0, y >= 0, w in 1..24 and x+w <= 24", i)
			}
			panel.X, panel.Width = g.X, g.W
		}
		panel.DataUpdatedAt = info.ModTime().UTC().Format(time.RFC3339)
		selected := []dataset.Record{}
		summaries := map[string]map[string]string{}
		summaryTimes := map[string]time.Time{}
		units := map[string]bool{}
		legends := map[string]string{}
		panel.Series = []map[string]string{}
		panel.Gaps = []MissingObservation{}
		latest := time.Time{}
		dateOnly := true
		queries := make([]Query, len(p.Targets))
		for i, target := range p.Targets {
			queries[i], _ = ParseQuery(target.Expr)
		}
		for _, row := range rows {
			name, _ := row.String("name")
			entity, _ := row.String("entity")
			for targetIndex, target := range p.Targets {
				q := queries[targetIndex]
				if !MatchRecordWithEntity(name, entity, q) {
					continue
				}
				stamp, _ := row.String("time")
				at, err := ParseTime(stamp)
				if err != nil {
					return DashboardView{}, fmt.Errorf("panel %d: invalid observation time %q", i, stamp)
				}
				if at.After(latest) {
					latest = at
					panel.LatestObservation = stamp
				}
				if unit, ok := row.String("unit"); ok && unit != "" {
					units[unit] = true
				}
				key := name + "\x00" + entity
				summary := map[string]string{"name": name, "metric": name, "entity": entity, "time": stamp}

				for field, value := range row {
					if field == "name" || field == "entity" || field == "time" || field == "value" || field == "version" || value == nil {
						continue
					}
					if text, ok := value.(string); ok {
						summary[field] = text
					} else {
						encoded, _ := json.Marshal(value)
						summary[field] = string(encoded)
					}
				}
				if !summaryTimes[key].After(at) {
					summaries[key] = summary
					summaryTimes[key] = at
				}
				if !since.IsZero() && at.Before(since) {
					break
				}
				// Copy before adding a display-only legend field; persisted observations never change.
				copy := dataset.Record{}
				for k, v := range row {
					copy[k] = v
				}
				title := target.LegendFormat
				if title == "" {
					title = strings.TrimSpace(entity + " " + name)
					if entity == "" {
						title = name
					}
				}
				_, labels, _ := SplitFolded(name)
				for k, v := range labels {
					title = strings.ReplaceAll(title, "{{"+k+"}}", v)
				}
				copy["_track_observed_time"] = stamp
				copy["time"] = at.UTC().Format("2006-01-02T15:04:05.000000000Z")
				if len(stamp) != len("2006-01-02") {
					dateOnly = false
				}
				href, _ := row.String("url")
				if href == "" {
					href, _ = row.String("href")
				}
				copy["_track_href"] = href
				label := strings.ReplaceAll(title, "{{entity}}", entity)
				if identity, ok := legends[label]; ok && identity != key {
					return DashboardView{}, fmt.Errorf("panel %d: legend labels collide; include distinguishing labels in legendFormat", i)
				}
				legends[label] = key
				copy["_track_series"] = label
				selected = append(selected, copy)
				panel.Points++
				if row["value"] == nil {
					panel.Missing++
					reason, _ := row.String("missing_reason")
					panel.Gaps = append(panel.Gaps, MissingObservation{Time: stamp, Entity: entity, Reason: reason})
				}
				break
			}
		}
		sort.SliceStable(selected, func(i, j int) bool { return selected[i]["time"].(string) < selected[j]["time"].(string) })
		if dateOnly {
			for _, row := range selected {
				row["time"] = row["time"].(string)[:10]
			}
		}
		if len(units) > 1 {
			return DashboardView{}, fmt.Errorf("panel %d combines different record units; split them across panels", i)
		}
		if panel.Unit == "" || panel.Unit == "none" {
			panel.Unit = ""
			for unit := range units {
				panel.Unit = unit
			}
		}
		keys := make([]string, 0, len(summaries))
		for key := range summaries {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			panel.Series = append(panel.Series, summaries[key])
		}
		spec.Data.Source, spec.Data.Records, spec.Filter = "", selected, nil
		spec.Encoding.Y = spec.Encoding.Y[:1]
		spec.Encoding.Y[0].AxisName = panel.Unit
		if p.FieldConfig.Defaults.Min != nil && p.FieldConfig.Defaults.Max != nil {
			spec.Encoding.Y[0].Domain = []float64{*p.FieldConfig.Defaults.Min, *p.FieldConfig.Defaults.Max}
		}
		spec.Encoding.Color = &viewspec.Channel{Field: "_track_series", Type: viewspec.Nominal}
		spec.Encoding.Detail = []viewspec.Channel{{Field: "_track_observed_time", Title: "Observation time as supplied"}, {Field: "unit", Title: "Unit"}, {Field: "missing_reason", Title: "Missing reason"}, {Field: "available_at", Title: "Available at"}, {Field: "fetched_at", Title: "Fetched at"}, {Field: "url", Title: "Source"}}
		for _, field := range []string{"frequency", "publication_frequency", "scope", "source", "asof", "revision", "detail", "provider", "definition"} {
			spec.Encoding.Detail = append(spec.Encoding.Detail, viewspec.Channel{Field: field, Title: field})
		}
		spec.Encoding.Href = &viewspec.Channel{Field: "_track_href"}
		validationSpec := spec
		if len(selected) == 0 {
			validationSpec.Data.Source = file
		}
		if err := validationSpec.Validate(); err != nil {
			return DashboardView{}, err
		}
		resolved := spec.Resolve(selected)
		option, err := render.EChartsOptionJSON(resolved)
		if err != nil {
			return DashboardView{}, err
		}
		// One-sided Grafana bounds cannot be expressed in a View Spec domain.
		var opt map[string]any
		_ = json.Unmarshal([]byte(option), &opt)
		if axes, ok := opt["yAxis"].([]any); ok && len(axes) > 0 {
			axis, _ := axes[0].(map[string]any)
			if axis != nil {
				if p.FieldConfig.Defaults.Min != nil {
					axis["min"] = *p.FieldConfig.Defaults.Min
				}
				if p.FieldConfig.Defaults.Max != nil {
					axis["max"] = *p.FieldConfig.Defaults.Max
				}
			}
		}
		panel.ECharts, _ = json.Marshal(opt)
		result.Panels = append(result.Panels, panel)
	}
	return result, nil
}

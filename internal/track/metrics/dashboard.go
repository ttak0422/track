package metrics

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/track/dataset"
	"github.com/ttak0422/track/internal/track/render"
	"github.com/ttak0422/track/internal/track/viewspec"
)

const (
	PanelTimeseries = "timeseries"
	PanelStat       = "stat"
	PanelTable      = "table"
	DashboardLang   = "metrics-dashboard"
)

// GrafanaDatasource addresses a metric JSONL file in the selected vault, never a remote service.
type GrafanaDatasource struct {
	Type string `json:"type"`
	UID  string `json:"uid"`
}
type grafanaTarget struct {
	Expr         string             `json:"expr"`
	LegendFormat string             `json:"legendFormat,omitempty"`
	Datasource   *GrafanaDatasource `json:"datasource,omitempty"`
}
type grafanaThresholdStep struct {
	Value *float64 `json:"value"`
}
type grafanaThresholds struct {
	Mode  string                 `json:"mode,omitempty"`
	Steps []grafanaThresholdStep `json:"steps"`
}
type grafanaFieldDefaults struct {
	Unit       string             `json:"unit,omitempty"`
	Decimals   *int               `json:"decimals,omitempty"`
	Min        *float64           `json:"min,omitempty"`
	Max        *float64           `json:"max,omitempty"`
	Thresholds *grafanaThresholds `json:"thresholds,omitempty"`
}

// PanelGrid uses Grafana's 24-column coordinates; readers stack panels on narrow screens.
type PanelGrid struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}
type grafanaPanel struct {
	Type        string             `json:"type"`
	Title       string             `json:"title"`
	GridPos     *PanelGrid         `json:"gridPos,omitempty"`
	Targets     []grafanaTarget    `json:"targets"`
	Datasource  *GrafanaDatasource `json:"datasource,omitempty"`
	FieldConfig struct {
		Defaults grafanaFieldDefaults `json:"defaults"`
	} `json:"fieldConfig"`
	Transformations []json.RawMessage `json:"transformations,omitempty"`
	Options         struct {
		ReduceOptions struct {
			Calcs []string `json:"calcs"`
		} `json:"reduceOptions"`
	} `json:"options"`
}
type grafanaDashboard struct {
	Title      string         `json:"title"`
	Panels     []grafanaPanel `json:"panels"`
	Templating struct {
		List []json.RawMessage `json:"list"`
	} `json:"templating"`
}

// DashboardSelection applies to every panel. Date-only bounds include the whole UTC day.
type DashboardSelection struct {
	From   string `json:"from,omitempty"`
	To     string `json:"to,omitempty"`
	Entity string `json:"entity,omitempty"`
	Metric string `json:"metric,omitempty"`
}
type DashboardValue struct {
	Name             string   `json:"name"`
	Entity           string   `json:"entity"`
	Value            float64  `json:"value"`
	Time             string   `json:"time"`
	Display          string   `json:"display"`
	Threshold        *float64 `json:"threshold,omitempty"`
	ThresholdDisplay string   `json:"thresholdDisplay,omitempty"`
}
type DashboardPanel struct {
	ID      string           `json:"id"`
	Title   string           `json:"title"`
	Type    string           `json:"type"`
	Grid    PanelGrid        `json:"grid"`
	Unit    string           `json:"unit"`
	Values  []DashboardValue `json:"values"`
	ECharts json.RawMessage  `json:"echarts,omitempty"`
	Error   string           `json:"error,omitempty"`
}
type Dashboard struct {
	Title    string           `json:"title"`
	Entities []string         `json:"entities"`
	Asof     string           `json:"asof"`
	Panels   []DashboardPanel `json:"panels"`
	Metrics  []string         `json:"metrics"`
}

func parseDashboard(data []byte) (grafanaDashboard, error) {
	var d grafanaDashboard
	if err := json.Unmarshal(data, &d); err != nil {
		return d, fmt.Errorf("parse dashboard JSON: %w", err)
	}
	if strings.TrimSpace(d.Title) == "" {
		return d, fmt.Errorf("dashboard needs a title")
	}
	if len(d.Panels) == 0 || len(d.Panels) > 100 {
		return d, fmt.Errorf("dashboard needs 1–100 panels")
	}
	if len(d.Templating.List) > 0 {
		return d, fmt.Errorf("templating variables are outside the subset; use the common target selector")
	}
	bottom := 0
	for i := range d.Panels {
		p := &d.Panels[i]
		fail := func(s string) (grafanaDashboard, error) { return d, fmt.Errorf("panel %d %q: %s", i, p.Title, s) }
		if p.Type != PanelTimeseries && p.Type != PanelStat && p.Type != PanelTable {
			return fail("type is outside the subset (want timeseries|stat|table)")
		}
		if len(p.Targets) == 0 {
			return fail("no targets")
		}
		if p.GridPos == nil {
			p.GridPos = &PanelGrid{W: 24, H: 8, Y: bottom}
		}
		g := p.GridPos
		if g.X < 0 || g.X > 23 || g.Y < 0 || g.W < 1 || g.W > 24 || g.H < 1 || g.W > 24-g.X || g.H > 100 || g.Y > 10000 {
			return fail("invalid gridPos (24 columns, positive width/height)")
		}
		bottom = max(bottom, g.Y+g.H)
		f := p.FieldConfig.Defaults
		if f.Decimals != nil && (*f.Decimals < 0 || *f.Decimals > 8) {
			return fail("decimals must be 0–8")
		}
		if f.Min != nil && f.Max != nil && *f.Min >= *f.Max {
			return fail("min must be less than max")
		}
		if f.Thresholds != nil && f.Thresholds.Mode != "" && f.Thresholds.Mode != "absolute" {
			return fail("only absolute thresholds are supported")
		}
		if len(p.Transformations) > 0 {
			return fail("transformations are outside the subset; compute upstream")
		}
		for _, calc := range p.Options.ReduceOptions.Calcs {
			if calc != "lastNotNull" && calc != "last" {
				return fail("only last/lastNotNull reduction is supported")
			}
		}
		for _, t := range p.Targets {
			if _, err := ParseQuery(t.Expr); err != nil {
				return fail(err.Error())
			}
			ds := t.Datasource
			if ds == nil {
				ds = p.Datasource
			}
			if ds == nil || ds.Type != "track" || !isBareFilename(ds.UID) {
				return fail("datasource must be {type:track,uid:<bare data filename>}")
			}
		}
	}
	return d, nil
}

// ResolveDashboard retains the dashboard as one interactive block rather than flattening stat
// panels and grid positions into unrelated line charts. CLI conversion validates its data first.
func ResolveDashboard(data []byte, dataDir string) (string, int, error) {
	d, err := ResolveDashboardView(data, dataDir, DashboardSelection{})
	if err != nil {
		return "", 0, err
	}
	for _, p := range d.Panels {
		if p.Error != "" {
			return "", 0, fmt.Errorf("panel %q: %s", p.Title, p.Error)
		}
	}
	var raw any
	if err = json.Unmarshal(data, &raw); err != nil {
		return "", 0, err
	}
	pretty, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return "", 0, err
	}
	return "```" + DashboardLang + "\n" + string(pretty) + "\n```\n", len(d.Panels), nil
}

// ResolveDashboardView is shared by Web and Native. File failures belong to their panel; invalid
// configuration belongs to the dashboard. Latest values are selected after filtering, never summed.
func ResolveDashboardView(data []byte, dataDir string, selection DashboardSelection) (Dashboard, error) {
	result := Dashboard{Entities: []string{}, Metrics: []string{}, Panels: []DashboardPanel{}}
	d, err := parseDashboard(data)
	if err != nil {
		return result, err
	}
	from, to, err := dashboardBounds(selection)
	if err != nil {
		return result, err
	}
	result.Title = d.Title
	type loaded struct {
		rows []dataset.Record
		err  error
	}
	files := map[string]loaded{}
	entities := map[string]bool{}
	metricNames := map[string]bool{}
	var newest time.Time
	for i, p := range d.Panels {
		out := DashboardPanel{ID: strconv.Itoa(i), Title: p.Title, Type: p.Type, Grid: *p.GridPos, Unit: p.FieldConfig.Defaults.Unit, Values: []DashboardValue{}}
		if out.Title == "" {
			out.Title = fmt.Sprintf("Panel %d", i+1)
		}
		rows := []dataset.Record{}
		latest := map[string]dataset.Record{}
		seen := map[string]bool{}
		labels := map[string]string{}
		for _, target := range p.Targets {
			ds := target.Datasource
			if ds == nil {
				ds = p.Datasource
			}
			f, ok := files[ds.UID]
			if !ok {
				f.rows, f.err = loadMetricFile(dataDir, ds.UID)
				files[ds.UID] = f
			}
			if f.err != nil {
				out.Error = f.err.Error()
				break
			}
			q, _ := ParseQuery(target.Expr)
			for _, r := range f.rows {
				name, _ := r.String("name")
				entity, _ := r.String("entity")
				stamp, _ := r.String("time")
				if !MatchRecordWithEntity(name, entity, q) {
					continue
				}
				if entity != "" {
					entities[entity] = true
				}
				metricNames[name] = true
				t, _ := ParseTime(stamp) // loadMetricFile validates timestamps.
				if selection.Metric != "" && selection.Metric != name || selection.Entity != "" && selection.Entity != entity || !from.IsZero() && t.Before(from) || !to.IsZero() && t.After(to) {
					continue
				}
				key := ds.UID + "\x00" + name + "\x00" + entity
				sample := key + "\x00" + t.UTC().Format(time.RFC3339Nano)
				if seen[sample] {
					continue
				}
				seen[sample] = true
				if _, ok := labels[key]; !ok {
					labels[key] = dashboardLegend(target, name, entity)
				}
				v, _ := r.Float("value")
				row := dataset.Record{"version": 1, "name": name, "entity": entity, "time": t.UTC().Format("2006-01-02T15:04:05.000000000Z"), "value": v, "series": key}
				rows = append(rows, row)
				prev, ok := latest[key]
				if !ok {
					latest[key] = r
				} else {
					prevStamp, _ := prev.String("time")
					prevTime, _ := ParseTime(prevStamp)
					if t.After(prevTime) {
						latest[key] = r
					}
				}
			}
		}
		if out.Error == "" {
			keys := make([]string, 0, len(latest))
			for k := range latest {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, key := range keys {
				r := latest[key]
				name, _ := r.String("name")
				entity, _ := r.String("entity")
				stamp, _ := r.String("time")
				v, _ := r.Float("value")
				val := DashboardValue{Name: name, Entity: entity, Value: v, Time: stamp, Display: dashboardDisplay(v, p.FieldConfig.Defaults)}
				if th := p.FieldConfig.Defaults.Thresholds; th != nil {
					for _, s := range th.Steps {
						if s.Value != nil && v >= *s.Value && (val.Threshold == nil || *s.Value > *val.Threshold) {
							val.Threshold = s.Value
						}
					}
				}
				if val.Threshold != nil {
					val.ThresholdDisplay = dashboardDisplay(*val.Threshold, p.FieldConfig.Defaults)
				}
				out.Values = append(out.Values, val)
				t, _ := ParseTime(stamp)
				if t.After(newest) {
					newest = t
					result.Asof = stamp
				}
			}
			if p.Type == PanelTimeseries && len(rows) > 0 {
				out.ECharts, err = dashboardChart(p, rows, labels)
				if err != nil {
					out.Error = err.Error()
				}
			}
		}
		result.Panels = append(result.Panels, out)
	}
	for e := range entities {
		result.Entities = append(result.Entities, e)
	}
	sort.Strings(result.Entities)
	for name := range metricNames {
		result.Metrics = append(result.Metrics, name)
	}
	sort.Strings(result.Metrics)
	sort.SliceStable(result.Panels, func(i, j int) bool {
		a, b := result.Panels[i].Grid, result.Panels[j].Grid
		if a.Y != b.Y {
			return a.Y < b.Y
		}
		return a.X < b.X
	})
	return result, nil
}

func dashboardBounds(s DashboardSelection) (time.Time, time.Time, error) {
	var from, to time.Time
	var err error
	if strings.TrimSpace(s.From) != "" {
		from, err = ParseTime(s.From)
		if err != nil {
			return from, to, fmt.Errorf("from: %w", err)
		}
	}
	if strings.TrimSpace(s.To) != "" {
		to, err = ParseTime(s.To)
		if err != nil {
			return from, to, fmt.Errorf("to: %w", err)
		}
		if len(strings.TrimSpace(s.To)) == 10 {
			to = to.AddDate(0, 0, 1).Add(-time.Nanosecond)
		}
	}
	if !from.IsZero() && !to.IsZero() && from.After(to) {
		return from, to, fmt.Errorf("from must not be after to")
	}
	return from, to, nil
}

func dashboardLegend(target grafanaTarget, name, entity string) string {
	title := strings.TrimSpace(target.LegendFormat)
	if title == "" {
		return strings.TrimSpace(entity + " " + name)
	}
	_, labels, _ := SplitFolded(name)
	for k, v := range labels {
		title = strings.ReplaceAll(title, "{{"+k+"}}", v)
	}
	return strings.ReplaceAll(title, "{{entity}}", entity)
}

func dashboardUnit(unit string) (float64, string) {
	switch unit {
	case "percentunit":
		return 100, "%"
	case "percent":
		return 1, "%"
	case "none", "short":
		return 1, ""
	case "bytes":
		return 1, "B"
	}
	return 1, unit
}

func dashboardDisplay(v float64, f grafanaFieldDefaults) string {
	decimals := 2
	if f.Decimals != nil {
		decimals = *f.Decimals
	}
	factor, unit := dashboardUnit(f.Unit)
	out := strconv.FormatFloat(v*factor, 'f', decimals, 64)
	if unit != "" {
		out += " " + unit
	}
	return out
}

func dashboardChart(p grafanaPanel, rows []dataset.Record, labels map[string]string) (json.RawMessage, error) {
	// Nominal color splitting already creates one line per series. Disambiguate repeated legends
	// rather than merging unrelated series that happen to share a display name.
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	used := map[string]bool{}
	for _, k := range keys {
		label := labels[k]
		for n := 2; used[label]; n++ {
			label = fmt.Sprintf("%s (%d)", labels[k], n)
		}
		used[label] = true
		labels[k] = label
	}
	factor, unit := dashboardUnit(p.FieldConfig.Defaults.Unit)
	for _, r := range rows {
		v, _ := r.Float("value")
		r["value"] = v * factor
		k, _ := r.String("series")
		r["series"] = labels[k]
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, _ := rows[i].String("time")
		b, _ := rows[j].String("time")
		at, _ := ParseTime(a)
		bt, _ := ParseTime(b)
		return at.Before(bt)
	})
	spec := viewspec.Spec{Version: 2, Mark: viewspec.MarkLine, Data: viewspec.DataRef{Kind: dataset.KindMetric, Records: rows}, Encoding: viewspec.Encoding{X: viewspec.Channel{Field: "time", Title: "Time"}, Y: []viewspec.Channel{{Field: "value", Title: unit}}, Color: &viewspec.Channel{Field: "series", Type: viewspec.Nominal}}}
	if th := p.FieldConfig.Defaults.Thresholds; th != nil {
		for _, s := range th.Steps {
			if s.Value != nil {
				v := *s.Value * factor
				spec.Overlays = append(spec.Overlays, viewspec.Overlay{Y: &v, Label: "≥ " + dashboardDisplay(*s.Value, p.FieldConfig.Defaults)})
			}
		}
	}
	raw, err := json.Marshal(spec)
	if err != nil {
		return nil, err
	}
	option, err := render.EChartsOptionFromSpecDir(raw, "")
	if err != nil {
		return nil, err
	}
	// Grafana permits either axis bound independently; viewspec's domain requires a pair.
	var opt map[string]any
	if err = json.Unmarshal([]byte(option), &opt); err != nil {
		return nil, err
	}
	if axes, ok := opt["yAxis"].([]any); ok && len(axes) > 0 {
		if axis, ok := axes[0].(map[string]any); ok {
			if p.FieldConfig.Defaults.Min != nil {
				axis["min"] = *p.FieldConfig.Defaults.Min * factor
			}
			if p.FieldConfig.Defaults.Max != nil {
				axis["max"] = *p.FieldConfig.Defaults.Max * factor
			}
		}
	}
	return json.Marshal(opt)
}

func isBareFilename(s string) bool {
	return s != "" && s != "." && s != ".." && s == filepath.Base(s) && !strings.ContainsAny(s, `\`+"\x00")
}

func loadMetricFile(dataDir, file string) ([]dataset.Record, error) {
	if !isBareFilename(file) {
		return nil, fmt.Errorf("invalid data filename %q", file)
	}
	root, err := os.OpenRoot(dataDir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.Open(file)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", file, err)
	}
	defer f.Close()
	rows, err := dataset.ReadJSONL(f)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", file, err)
	}
	if err = dataset.ValidateRecords(dataset.KindMetric, rows); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	seen := map[string]bool{}
	for _, r := range rows {
		value, _ := r.Float("value")
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, fmt.Errorf("%s: metric value must be finite", file)
		}
		stamp, _ := r.String("time")
		t, err := ParseTime(stamp)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
		name, _ := r.String("name")
		ent, _ := r.String("entity")
		key := name + "\x00" + ent + "\x00" + t.UTC().Format(time.RFC3339Nano)
		if seen[key] {
			return nil, fmt.Errorf("%s: duplicate sample for %s %s at %s", file, name, ent, stamp)
		}
		seen[key] = true
	}
	return rows, nil
}

package metrics

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/ttak0422/track/internal/track/dataset"
)

// RuleFile is the Prometheus rule-YAML subset: groups of alert rules. Per rule only alert, expr,
// for, labels, and annotations are read; anything else is ignored. expr is `metric{matchers} OP
// number` (see ParseComparison); for is a count of consecutive trailing points that must all hold.
type RuleFile struct {
	Groups []RuleGroup `yaml:"groups"`
}

// RuleGroup groups rules under a name.
type RuleGroup struct {
	Name  string      `yaml:"name"`
	Rules []AlertRule `yaml:"rules"`
}

// AlertRule is one threshold rule.
type AlertRule struct {
	Alert       string            `yaml:"alert"`
	Expr        string            `yaml:"expr"`
	For         int               `yaml:"for"`
	Labels      map[string]string `yaml:"labels"`
	Annotations map[string]string `yaml:"annotations"`
}

// Firing is one alert whose condition holds on current data.
type Firing struct {
	Alert       string            `json:"alert"`
	Metric      string            `json:"metric"`
	Entity      string            `json:"entity,omitempty"`
	Value       float64           `json:"value"`
	Time        string            `json:"time"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// ParseRules parses rule YAML, failing loudly on anything outside the subset.
func ParseRules(data []byte) (RuleFile, error) {
	var rf RuleFile
	if err := yaml.Unmarshal(data, &rf); err != nil {
		return RuleFile{}, fmt.Errorf("parse rules: %w", err)
	}
	for gi, g := range rf.Groups {
		for ri, r := range g.Rules {
			if strings.TrimSpace(r.Alert) == "" {
				return RuleFile{}, fmt.Errorf("groups[%d].rules[%d]: alert needs a name", gi, ri)
			}
			if _, err := ParseComparison(r.Expr); err != nil {
				return RuleFile{}, fmt.Errorf("groups[%d].rules[%d] %q: %w", gi, ri, r.Alert, err)
			}
			if r.For < 0 {
				return RuleFile{}, fmt.Errorf("groups[%d].rules[%d] %q: for must be >= 0", gi, ri, r.Alert)
			}
		}
	}
	return rf, nil
}

type point struct {
	time   string
	value  float64
	name   string
	entity string
}

// EvalRules evaluates every rule against the latest values of every *.jsonl under dataDir (metric
// rows are those carrying name/time/value). A rule fires when its comparison holds on the trailing
// N points of a series, where N is for (for <= 0 means the latest point only).
func EvalRules(dataDir string, rf RuleFile) ([]Firing, error) {
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return nil, fmt.Errorf("read data dir: %w", err)
	}
	series := map[string][]point{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		f, err := os.Open(filepath.Join(dataDir, e.Name()))
		if err != nil {
			return nil, err
		}
		rows, err := dataset.ReadJSONL(f)
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		for _, r := range rows {
			nm, ok1 := r.String("name")
			tm, ok2 := r.String("time")
			v, ok3 := r.Float("value")
			if !ok1 || !ok2 || !ok3 {
				continue
			}
			ent, _ := r.String("entity")
			// The series key carries the entity: two symbols sharing a gauge name stay separate.
			series[nm+"\x00"+ent] = append(series[nm+"\x00"+ent], point{time: tm, value: v, name: nm, entity: ent})
		}
	}
	for k := range series {
		pts := series[k]
		sort.Slice(pts, func(i, j int) bool { return pts[i].time < pts[j].time })
		series[k] = pts
	}
	names := make([]string, 0, len(series))
	for k := range series {
		names = append(names, k)
	}
	sort.Strings(names)
	var firing []Firing
	for _, g := range rf.Groups {
		for _, r := range g.Rules {
			cmp, err := ParseComparison(r.Expr)
			if err != nil {
				return nil, err // validated at parse; kept for safety
			}
			n := r.For
			if n <= 0 {
				n = 1
			}
			for _, key := range names {
				pts := series[key]
				if !MatchRecord(pts[0].name, cmp.Query) {
					continue
				}
				if len(pts) < n {
					continue
				}
				tail := pts[len(pts)-n:]
				holds := true
				for _, p := range tail {
					if !Compare(cmp.Op, p.value, cmp.Value) {
						holds = false
						break
					}
				}
				if !holds {
					continue
				}
				last := tail[len(tail)-1]
				f := Firing{
					Alert:  r.Alert,
					Metric: last.name,
					Entity: last.entity,
					Value:  last.value,
					Time:   last.time,
				}
				if f.Entity == "" {
					if _, labels, err := SplitFolded(last.name); err == nil {
						if e, ok := entityOf(labels); ok {
							f.Entity = e
						}
					}
				}
				if len(r.Labels) > 0 {
					f.Labels = r.Labels
				}
				if len(r.Annotations) > 0 {
					f.Annotations = r.Annotations
				}
				firing = append(firing, f)
			}
		}
	}
	return firing, nil
}

// entityOf recovers the entity label folded into a series name, if any.
func entityOf(labels map[string]string) (string, bool) {
	for _, k := range DefaultEntityLabels() {
		if v, ok := labels[k]; ok {
			return v, true
		}
	}
	return "", false
}

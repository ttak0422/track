package cli

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/track/dataset"
	"github.com/ttak0422/track/internal/track/link"
	"github.com/ttak0422/track/internal/track/metrics"
	"github.com/ttak0422/track/internal/track/note"
)

// cmdMetrics routes the metrics monitoring subcommands (ADR 0076): exposition ingest, gauge
// derivation, Grafana-subset dashboards, and rule-subset alert evaluation.
func cmdMetrics(args []string) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Print(metricsUsage())
		return 0
	}
	switch args[0] {
	case "scrape":
		return cmdMetricsScrape(args[1:])
	case "derive":
		return cmdMetricsDerive(args[1:])
	case "dashboard":
		return cmdMetricsDashboard(args[1:])
	case "alert":
		return cmdMetricsAlert(args[1:])
	default:
		return fail("unknown metrics subcommand %q (want scrape|derive|dashboard|alert)", args[0])
	}
}

func metricsUsage() string {
	return `track metrics - Grafana-style monitoring on adopted specs (ADR 0076)

  track metrics scrape --from <file|-> --out <file> [--entity-labels a,b] [--asof DATE]
                                        parse OpenMetrics/Prometheus exposition text into metric-kind JSONL
  track metrics derive --prices <file> --out <file> [--gauges a,b]
                                        derive gauges (change_pct, ma5_dev, ma25_dev, rsi14, mom_12_1, high52w)
                                        from any price-kind JSONL into metric-kind JSONL
  track metrics dashboard --dashboard <grafana.json> --out <note.md> [--data-dir DIR]
                                        resolve a Grafana-subset dashboard into note Markdown with viewspec fences
  track metrics alert --rules <rules.yaml> [--data-dir DIR] [--capture "Note#Heading"]
                                        evaluate Prometheus-subset threshold rules; --capture records firing alerts
`
}

// vaultDataDir resolves the data directory: an explicit --data-dir wins, else the active vault's
// data/ (metrics commands work on vault data by default but stay usable outside one).
func vaultDataDir(dataDir string) (string, error) {
	if strings.TrimSpace(dataDir) != "" {
		return dataDir, nil
	}
	cfg, s, err := open()
	if err != nil {
		return "", fmt.Errorf("no --data-dir and no vault: %v", err)
	}
	s.Close()
	return filepath.Join(cfg.VaultDir, "data"), nil
}

func writeJSONL(path string, recs []dataset.Record) error {
	var b bytes.Buffer
	for _, r := range recs {
		line, err := json.Marshal(r)
		if err != nil {
			return err
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	return os.WriteFile(path, b.Bytes(), 0o644)
}

func cmdMetricsScrape(args []string) int {
	fs := flag.NewFlagSet("metrics scrape", flag.ContinueOnError)
	from := fs.String("from", "", "exposition file, or - for stdin")
	out := fs.String("out", "", "metric-kind JSONL to write")
	entityLabels := fs.String("entity-labels", "entity,symbol,instance", "label search order for the entity field (comma-separated)")
	asof := fs.String("asof", "", "stamp for unstamped samples (YYYY-MM-DD or RFC3339; default now)")
	if code, ok := parseArgs(fs, args); !ok {
		return code
	}
	if *from == "" || *out == "" {
		return fail("--from and --out are required")
	}
	var in []byte
	var err error
	if *from == "-" {
		in, err = io.ReadAll(os.Stdin)
	} else {
		in, err = os.ReadFile(*from)
	}
	if err != nil {
		return fail("read input: %v", err)
	}
	_, stampStr, err := metrics.NormalizeAsof(*asof)
	if err != nil {
		return fail("%v", err)
	}
	stamp, _ := metrics.ParseTime(stampStr)
	recs, err := metrics.Scrape(bytes.NewReader(in), metrics.ScrapeOptions{
		EntityLabels: strings.Split(*entityLabels, ","),
		Stamp:        stamp,
	})
	if err != nil {
		return fail("%v", err)
	}
	if err := writeJSONL(*out, recs); err != nil {
		return fail("write: %v", err)
	}
	return emit(map[string]any{"path": *out, "records": len(recs)})
}

func cmdMetricsDerive(args []string) int {
	fs := flag.NewFlagSet("metrics derive", flag.ContinueOnError)
	prices := fs.String("prices", "", "price-kind JSONL (any writer)")
	out := fs.String("out", "", "metric-kind JSONL to write")
	gauges := fs.String("gauges", "", "subset to compute (comma-separated; default all)")
	if code, ok := parseArgs(fs, args); !ok {
		return code
	}
	if *prices == "" || *out == "" {
		return fail("--prices and --out are required")
	}
	raw, err := os.ReadFile(*prices)
	if err != nil {
		return fail("read prices: %v", err)
	}
	rows, err := dataset.ReadJSONL(bytes.NewReader(raw))
	if err != nil {
		return fail("parse prices: %v", err)
	}
	var want []string
	if strings.TrimSpace(*gauges) != "" {
		want = strings.Split(*gauges, ",")
	}
	recs, err := metrics.Derive(rows, want)
	if err != nil {
		return fail("%v", err)
	}
	if err := writeJSONL(*out, recs); err != nil {
		return fail("write: %v", err)
	}
	return emit(map[string]any{"path": *out, "records": len(recs)})
}

func cmdMetricsDashboard(args []string) int {
	fs := flag.NewFlagSet("metrics dashboard", flag.ContinueOnError)
	dashboard := fs.String("dashboard", "", "Grafana-subset dashboard JSON file")
	out := fs.String("out", "", "note Markdown to write")
	dataDir := fs.String("data-dir", "", "metric JSONL directory (default: active vault data/)")
	if code, ok := parseArgs(fs, args); !ok {
		return code
	}
	if *dashboard == "" || *out == "" {
		return fail("--dashboard and --out are required")
	}
	dir, err := vaultDataDir(*dataDir)
	if err != nil {
		return fail("%v", err)
	}
	raw, err := os.ReadFile(*dashboard)
	if err != nil {
		return fail("read dashboard: %v", err)
	}
	md, n, err := metrics.ResolveDashboard(raw, dir)
	if err != nil {
		return fail("%v", err)
	}
	if err := os.WriteFile(*out, []byte(md), 0o644); err != nil {
		return fail("write: %v", err)
	}
	return emit(map[string]any{"path": *out, "panels": n})
}

func cmdMetricsAlert(args []string) int {
	fs := flag.NewFlagSet("metrics alert", flag.ContinueOnError)
	rules := fs.String("rules", "", "Prometheus-subset rule YAML file")
	dataDir := fs.String("data-dir", "", "metric JSONL directory (default: active vault data/)")
	capture := fs.String("capture", "", "record firing alerts under \"Note#Heading\"")
	if code, ok := parseArgs(fs, args); !ok {
		return code
	}
	if *rules == "" {
		return fail("--rules is required")
	}
	dir, err := vaultDataDir(*dataDir)
	if err != nil {
		return fail("%v", err)
	}
	raw, err := os.ReadFile(*rules)
	if err != nil {
		return fail("read rules: %v", err)
	}
	rf, err := metrics.ParseRules(raw)
	if err != nil {
		return fail("%v", err)
	}
	firing, err := metrics.EvalRules(dir, rf)
	if err != nil {
		return fail("%v", err)
	}
	result := map[string]any{"firing": firing, "count": len(firing)}
	if strings.TrimSpace(*capture) != "" && len(firing) > 0 {
		cfg, s, err := open()
		if err != nil {
			return fail("%v", err)
		}
		defer s.Close()
		key, heading, level := link.SplitAnchor(strings.TrimSpace(*capture))
		if key == "" {
			return fail("invalid --capture %q (want \"Note#Heading\")", *capture)
		}
		notePath, err := resolveNotePath(cfg, s, 0, key, "")
		if err != nil {
			return fail("%v", err)
		}
		noteID, err := note.IDFromPath(notePath)
		if err != nil {
			return fail("invalid note path: %v", err)
		}
		var lines []string
		stamp := time.Now().Format("2006-01-02")
		for _, f := range firing {
			lines = append(lines, fmt.Sprintf("- [%s] %s %s=%v @ %s", stamp, f.Alert, f.Metric, f.Value, f.Time))
		}
		_, _, at, err := captureIntoNote(cfg, s, notePath, noteID, heading, level, strings.Join(lines, "\n"))
		if err != nil {
			return fail("%v", err)
		}
		result["captured"] = map[string]any{"target": *capture, "line": at}
	}
	return emit(result)
}

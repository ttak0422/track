package render

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	datamodel "github.com/ttak0422/track/internal/track/dataset"
	"github.com/ttak0422/track/internal/track/viewspec"
)

// EChartsOptionFromSpecDir resolves an embedded View Spec (a fenced ```viewspec block, or a
// ".viewspec.json" asset) to its ECharts option JSON, which a frontend hands to its own ECharts
// instance — chart semantics stay decided here. dataDir is the vault's canonical-data directory:
// data.source and overlay sources are JSONL paths resolved inside it, so a note-embedded chart can
// reference the vault's data/ files while inline data.records keeps working. An empty dataDir allows
// inline data only (the isolated-asset path); source marker overlays are likewise ignored there,
// while line/band overlays and inline marker records travel with the spec and always render.
func EChartsOptionFromSpecDir(specJSON []byte, dataDir string) (string, error) {
	res, err := resolveSpecDir(specJSON, dataDir)
	if err != nil {
		return "", err
	}
	return EChartsOptionJSON(res)
}

// resolveSpecDir loads and resolves an embedded View Spec against dataDir, the shared core of the
// embedded rendering paths (static SVG and the ECharts option endpoint).
func resolveSpecDir(specJSON []byte, dataDir string) (viewspec.Resolved, error) {
	vs, err := viewspec.Load(bytes.NewReader(specJSON))
	if err != nil {
		return viewspec.Resolved{}, err
	}
	records := vs.Data.Records
	if len(records) == 0 {
		if dataDir == "" {
			return viewspec.Resolved{}, fmt.Errorf("embedded chart requires inline data (data.records); data.source is not supported here")
		}
		if records, err = readJSONLIn(dataDir, vs.Data.Source); err != nil {
			return viewspec.Resolved{}, err
		}
	}
	if err := datamodel.ValidateRecords(vs.Data.Kind, records); err != nil {
		return viewspec.Resolved{}, fmt.Errorf("data: %w", err)
	}
	res := vs.Resolve(records)
	// Only source overlays read a file here (line/band literals and inline marker records resolve in
	// Resolve). They need a data directory; the isolated-asset path keeps ignoring them, matching the
	// documented asset-embed contract.
	for i, ov := range vs.Overlays {
		if ov.Source == "" || dataDir == "" {
			continue
		}
		ovRecords, err := readJSONLIn(dataDir, ov.Source)
		if err != nil {
			return viewspec.Resolved{}, fmt.Errorf("overlay[%d]: %w", i, err)
		}
		if err := datamodel.ValidateRecords(ov.Kind, ovRecords); err != nil {
			return viewspec.Resolved{}, fmt.Errorf("overlay[%d]: %w", i, err)
		}
		res.Markers = append(res.Markers, ov.Markers(ovRecords)...)
	}
	return res, nil
}

// readJSONLIn reads a JSONL data source confined to dir, so a spec written in a note cannot escape the
// vault's data directory via ".." or an absolute path (the same rule the site's asset collection uses).
func readJSONLIn(dir, source string) ([]datamodel.Record, error) {
	source = strings.TrimSpace(source)
	if filepath.IsAbs(source) || strings.Contains(source, "..") {
		return nil, fmt.Errorf("data source %q must be a relative path inside the vault data directory", source)
	}
	full := filepath.Join(dir, filepath.FromSlash(source))
	f, err := os.Open(full)
	if err != nil {
		return nil, fmt.Errorf("open data %s: %w", source, err)
	}
	defer f.Close()
	records, err := datamodel.ReadJSONL(f)
	if err != nil {
		return nil, fmt.Errorf("read data %s: %w", source, err)
	}
	return records, nil
}

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

// SVGFromSpec loads a single View Spec carrying inline data (data.records) and renders it to a static
// SVG. It is the engine behind embedding a ".viewspec.json" asset as a chart: a self-contained spec
// becomes an image at build/serve time, with no external data file and no client-side JavaScript.
//
// Embedded charts must carry their data inline; a spec that uses data.source (an external JSONL file)
// is rejected here, since an asset is rendered in isolation without a spec-relative file to read.
// Marker overlays (a second data source) are likewise out of scope for the embedded path; line/band
// overlays carry literal values and render.
func SVGFromSpec(specJSON []byte) (string, error) {
	return SVGFromSpecDir(specJSON, "")
}

// SVGFromSpecDir renders a View Spec embedded in a note (a fenced ```viewspec block) to a static SVG.
// dataDir is the vault's canonical-data directory: data.source and overlay sources are JSONL paths
// resolved inside it, so a note-embedded chart can reference the vault's data/ files while inline
// data.records keeps working. An empty dataDir allows inline data only (the isolated-asset path above).
func SVGFromSpecDir(specJSON []byte, dataDir string) (string, error) {
	vs, err := viewspec.Load(bytes.NewReader(specJSON))
	if err != nil {
		return "", err
	}
	records := vs.Data.Records
	if len(records) == 0 {
		if dataDir == "" {
			return "", fmt.Errorf("embedded chart requires inline data (data.records); data.source is not supported here")
		}
		if records, err = readJSONLIn(dataDir, vs.Data.Source); err != nil {
			return "", err
		}
	}
	if err := datamodel.ValidateRecords(vs.Data.Kind, records); err != nil {
		return "", fmt.Errorf("data: %w", err)
	}
	res := vs.Resolve(records)
	// Overlays read their own sources, so they only work when a data directory is available; the
	// isolated-asset path keeps ignoring them, matching the documented asset-embed contract.
	if dataDir != "" {
		for i, ov := range vs.Overlays {
			ovRecords, err := readJSONLIn(dataDir, ov.Source)
			if err != nil {
				return "", fmt.Errorf("overlay[%d]: %w", i, err)
			}
			if err := datamodel.ValidateRecords(ov.Kind, ovRecords); err != nil {
				return "", fmt.Errorf("overlay[%d]: %w", i, err)
			}
			res.Markers = append(res.Markers, ov.Markers(ovRecords)...)
		}
	}
	return SVG{}.Render(res)
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

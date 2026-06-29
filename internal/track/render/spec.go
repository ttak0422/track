package render

import (
	"bytes"
	"fmt"

	"github.com/ttak0422/track/internal/track/viewspec"
)

// SVGFromSpec loads a single View Spec carrying inline data (data.records) and renders it to a static
// SVG. It is the engine behind embedding a ".viewspec.json" asset as a chart: a self-contained spec
// becomes an image at build/serve time, with no external data file and no client-side JavaScript.
//
// Embedded charts must carry their data inline; a spec that uses data.source (an external JSONL file)
// is rejected here, since an asset is rendered in isolation without a spec-relative file to read.
// Overlays (a second data source) are likewise out of scope for the embedded path.
func SVGFromSpec(specJSON []byte) (string, error) {
	vs, err := viewspec.Load(bytes.NewReader(specJSON))
	if err != nil {
		return "", err
	}
	if len(vs.Data.Records) == 0 {
		return "", fmt.Errorf("embedded chart requires inline data (data.records); data.source is not supported here")
	}
	return SVG{}.Render(vs.Resolve(vs.Data.Records))
}

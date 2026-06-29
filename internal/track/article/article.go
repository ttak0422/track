// Package article defines a composed document: an ordered mix of Markdown prose and charts, rendered
// into a single HTML page. It is the "data + layout + rendering" unit of track's visualization goal —
// a declarative article spec that combines narrative with multiple View Spec charts.
//
// This package is the pure spec layer: parsing and validation only. Resolving each chart's data and
// producing HTML live in the CLI and the render package, so article stays free of file IO and any
// renderer.
package article

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/ttak0422/track/internal/track/viewspec"
)

// Version is the current article schema version. Specs carry it so the format can evolve.
const Version = 1

// Article is a composed document: a title and an ordered list of blocks.
type Article struct {
	Version int     `json:"version"`
	Title   string  `json:"title,omitempty"`
	Blocks  []Block `json:"blocks"`
}

// Block is one element of an article. Exactly one of Markdown, Chart, or Table is set: Markdown is a
// prose block, Chart is an inline View Spec, Table is an inline data table. Chart and Table data
// sources are resolved later, relative to the article file.
type Block struct {
	Markdown string          `json:"markdown,omitempty"`
	Chart    *viewspec.Spec  `json:"chart,omitempty"`
	Table    *viewspec.Table `json:"table,omitempty"`
}

// Load parses an article from JSON and validates it. Unknown fields are rejected so a typo surfaces as
// an error rather than being silently ignored.
func Load(r io.Reader) (Article, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	var a Article
	if err := dec.Decode(&a); err != nil {
		return Article{}, fmt.Errorf("decode article: %w", err)
	}
	if err := a.Validate(); err != nil {
		return Article{}, err
	}
	return a, nil
}

// Validate checks the article is renderable: a known version, at least one block, and every block is
// exactly one of prose or a (valid) chart.
func (a Article) Validate() error {
	if a.Version == 0 {
		return fmt.Errorf("article: missing version (current is %d)", Version)
	}
	if a.Version > Version {
		return fmt.Errorf("article: version %d is newer than supported %d", a.Version, Version)
	}
	if len(a.Blocks) == 0 {
		return fmt.Errorf("article: at least one block is required")
	}
	for i, b := range a.Blocks {
		set := 0
		if b.Markdown != "" {
			set++
		}
		if b.Chart != nil {
			set++
		}
		if b.Table != nil {
			set++
		}
		switch {
		case set > 1:
			return fmt.Errorf("article: blocks[%d] sets more than one of markdown/chart/table (pick one)", i)
		case set == 0:
			return fmt.Errorf("article: blocks[%d] is empty (set markdown, chart, or table)", i)
		case b.Chart != nil:
			if err := b.Chart.Validate(); err != nil {
				return fmt.Errorf("article: blocks[%d].chart: %w", i, err)
			}
		case b.Table != nil:
			if err := b.Table.Validate(); err != nil {
				return fmt.Errorf("article: blocks[%d].table: %w", i, err)
			}
		}
	}
	return nil
}

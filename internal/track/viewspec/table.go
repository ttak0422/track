package viewspec

import (
	"fmt"
	"strings"

	"github.com/ttak0422/track/internal/track/dataset"
)

// Table is a renderer-independent description of a data table over Canonical Data Model records: a
// data source and the columns (record fields) to show. Filter requests a client-side text filter box.
// Like Spec, it knows nothing about HTML; a renderer turns it into output.
type Table struct {
	Data    DataRef  `json:"data"`
	Columns []Column `json:"columns"`
	Filter  bool     `json:"filter,omitempty"`
}

// Column binds a table column to a record field, with an optional header label (defaults to the field).
type Column struct {
	Field string `json:"field"`
	Label string `json:"label,omitempty"`
}

func (c Column) label() string {
	if c.Label != "" {
		return c.Label
	}
	return c.Field
}

// Validate checks a table is renderable: a data source with a known kind and at least one column.
func (t Table) Validate() error {
	if strings.TrimSpace(t.Data.Source) == "" {
		return fmt.Errorf("table: data.source is required")
	}
	if !t.Data.Kind.Valid() {
		return fmt.Errorf("table: data.kind %q is not a canonical kind", t.Data.Kind)
	}
	if len(t.Columns) == 0 {
		return fmt.Errorf("table: at least one column is required")
	}
	for i, c := range t.Columns {
		if c.Field == "" {
			return fmt.Errorf("table: columns[%d].field is required", i)
		}
	}
	return nil
}

// ResolvedTable is a Table applied to data: header labels and string cells, ready for a renderer. A
// renderer consumes this and never touches raw records.
type ResolvedTable struct {
	Columns []string
	Rows    [][]string
	Filter  bool
}

// Resolve projects records onto the table's columns, one row per record. A missing cell is the empty
// string so the row still aligns to the header.
func (t Table) Resolve(records []dataset.Record) ResolvedTable {
	out := ResolvedTable{Filter: t.Filter}
	for _, c := range t.Columns {
		out.Columns = append(out.Columns, c.label())
	}
	for _, rec := range records {
		row := make([]string, len(t.Columns))
		for i, c := range t.Columns {
			row[i], _ = rec.String(c.Field)
		}
		out.Rows = append(out.Rows, row)
	}
	return out
}

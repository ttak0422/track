package query

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMarkdownStableIDLinksKeepTableCellsIntact(t *testing.T) {
	res := Result{Columns: []string{"title", "tags"}, Rows: []Row{
		{NoteID: 42, Title: "Alpha", Cells: []string{"Alpha", "tag"}},
		{NoteID: 43, Title: "A | B", Cells: []string{"A | B", ""}},
		{NoteID: 44, Title: "42", Cells: []string{"42", ""}},
	}}
	got := Markdown(res, "id-v1")
	for _, want := range []string{"| [[id:42]] | tag |", "| [[id:43]] |  |", "| [[id:44]] |  |"} {
		if !strings.Contains(got, want) {
			t.Fatalf("stable table missing %q:\n%s", want, got)
		}
	}
	// Existing callers and explicit title mode keep their old output.
	for _, got := range []string{Markdown(res), Markdown(res, "title")} {
		for _, want := range []string{"[[Alpha]]", `A \| B`, "[[42]]"} {
			if !strings.Contains(got, want) {
				t.Fatalf("legacy table missing %q:\n%s", want, got)
			}
		}
		if strings.Contains(got, "id:") {
			t.Fatalf("legacy table exposed a stable target: %s", got)
		}
	}
}

func TestExpandBlocksStableIDLinks(t *testing.T) {
	got := ExpandBlocks("```track-query\nTABLE title\n```", nil, viewRows(), nil, "id-v1")
	if !strings.Contains(got, "[[id:1]]") || strings.Contains(got, "[[Alpha]]") {
		t.Fatalf("stable table expansion = %q", got)
	}
	for _, layout := range []string{"list", "gallery", "board", "calendar"} {
		t.Run(layout, func(t *testing.T) {
			columns := "TABLE title"
			if layout == "board" {
				columns += ", props.state"
			} else if layout == "calendar" {
				columns += ", props.due"
			}
			body := "```track-query :layout " + layout + "\n" + columns + "\n```"
			got := ExpandBlocksWithResolver(body, nil, viewRows(), nil, nil, "id-v1")
			var view View
			if err := json.Unmarshal([]byte(strings.Split(got, "\n")[1]), &view); err != nil {
				t.Fatalf("decode view %q: %v", got, err)
			}
			for _, group := range view.Groups {
				for _, row := range group.Rows {
					want := map[string]string{"Alpha": "id:1", "Beta": "id:2", "Gamma": "id:3", "Delta": "id:4"}[row.Title]
					if row.Target != want {
						t.Fatalf("row = %+v, want target %s", row, want)
					}
				}
			}
			legacy := ExpandBlocks(body, nil, viewRows(), nil)
			if strings.Contains(legacy, `"target"`) {
				t.Fatalf("legacy view gained target field: %s", legacy)
			}
		})
	}
}

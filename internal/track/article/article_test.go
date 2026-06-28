package article

import (
	"strings"
	"testing"
)

const goodArticle = `{
  "version": 1,
  "title": "Narrative",
  "blocks": [
    { "markdown": "# Hi" },
    { "chart": { "version": 1, "type": "line", "data": {"source":"m.jsonl","kind":"metric"},
                 "x": {"field":"time"}, "y": [{"field":"value"}] } }
  ]
}`

func TestLoadValid(t *testing.T) {
	a, err := Load(strings.NewReader(goodArticle))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if a.Title != "Narrative" || len(a.Blocks) != 2 {
		t.Fatalf("unexpected article: %+v", a)
	}
	if a.Blocks[0].Markdown == "" || a.Blocks[1].Chart == nil {
		t.Fatalf("blocks not parsed: %+v", a.Blocks)
	}
}

func TestValidateErrors(t *testing.T) {
	cases := map[string]string{
		"missing version": `{"blocks":[{"markdown":"x"}]}`,
		"no blocks":       `{"version":1,"blocks":[]}`,
		"empty block":     `{"version":1,"blocks":[{}]}`,
		"both set":        `{"version":1,"blocks":[{"markdown":"x","chart":{"version":1,"type":"line","data":{"source":"m","kind":"metric"},"x":{"field":"t"},"y":[{"field":"v"}]}}]}`,
		"bad chart":       `{"version":1,"blocks":[{"chart":{"version":1,"type":"pie","data":{"source":"m","kind":"metric"},"x":{"field":"t"},"y":[{"field":"v"}]}}]}`,
	}
	for name, body := range cases {
		if _, err := Load(strings.NewReader(body)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	if _, err := Load(strings.NewReader(`{"version":1,"blocks":[{"markdown":"x"}],"bogus":1}`)); err == nil {
		t.Fatal("expected unknown-field error")
	}
}

package render

import (
	"strings"
	"testing"
)

func TestSVGFromSpecInlineData(t *testing.T) {
	spec := `{"version":2,"mark":"bar","title":"Inline","data":{"kind":"metric","records":[
		{"name":"A","time":"t1","value":3},{"name":"B","time":"t1","value":7}]},
		"encoding":{"x":{"field":"name","type":"nominal"},"y":[{"field":"value"}]}}`
	out, err := SVGFromSpec([]byte(spec))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "<?xml") || !strings.Contains(out, "<svg") {
		t.Fatalf("not an SVG document: %.40s", out)
	}
	for _, want := range []string{">Inline<", ">A<", ">B<", "<rect"} {
		if !strings.Contains(out, want) {
			t.Errorf("SVG missing %q", want)
		}
	}
}

func TestSVGFromSpecRejectsNonConformantData(t *testing.T) {
	// kind metric requires name/time/value; these records have none of them.
	spec := `{"version":2,"mark":"bar","data":{"kind":"metric","records":[{"x":"A","y":3}]},
		"encoding":{"x":{"field":"x","type":"nominal"},"y":[{"field":"y"}]}}`
	if _, err := SVGFromSpec([]byte(spec)); err == nil {
		t.Fatal("expected non-conformant canonical data to be rejected")
	}
}

func TestSVGFromSpecRequiresInlineData(t *testing.T) {
	// data.source (external file) is not supported by the embedded path.
	spec := `{"version":2,"mark":"line","data":{"kind":"metric","source":"x.jsonl"},
		"encoding":{"x":{"field":"t"},"y":[{"field":"v"}]}}`
	if _, err := SVGFromSpec([]byte(spec)); err == nil {
		t.Fatal("expected error: embedded chart needs inline data")
	}
}

func TestSVGFromSpecRejectsBadSpec(t *testing.T) {
	if _, err := SVGFromSpec([]byte(`{"version":2,"mark":"pie"}`)); err == nil {
		t.Fatal("expected error for invalid spec")
	}
}

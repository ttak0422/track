package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOptionFromSpecInlineData(t *testing.T) {
	spec := `{"version":2,"mark":"bar","title":"Inline","data":{"kind":"metric","records":[
		{"name":"A","time":"t1","value":3},{"name":"B","time":"t1","value":7}]},
		"encoding":{"x":{"field":"name","type":"nominal"},"y":[{"field":"value"}]}}`
	out, err := EChartsOptionFromSpecDir([]byte(spec), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"text":"Inline"`, `"data":["A","B"]`, `"type":"bar"`} {
		if !strings.Contains(out, want) {
			t.Errorf("option missing %q", want)
		}
	}
}

func TestOptionFromSpecRejectsNonConformantData(t *testing.T) {
	// kind metric requires name/time/value; these records have none of them.
	spec := `{"version":2,"mark":"bar","data":{"kind":"metric","records":[{"x":"A","y":3}]},
		"encoding":{"x":{"field":"x","type":"nominal"},"y":[{"field":"y"}]}}`
	if _, err := EChartsOptionFromSpecDir([]byte(spec), ""); err == nil {
		t.Fatal("expected non-conformant canonical data to be rejected")
	}
}

func TestOptionFromSpecRequiresInlineData(t *testing.T) {
	// data.source (external file) is not supported by the embedded path.
	spec := `{"version":2,"mark":"line","data":{"kind":"metric","source":"x.jsonl"},
		"encoding":{"x":{"field":"t"},"y":[{"field":"v"}]}}`
	if _, err := EChartsOptionFromSpecDir([]byte(spec), ""); err == nil {
		t.Fatal("expected error: embedded chart needs inline data")
	}
}

func TestOptionFromSpecRejectsBadSpec(t *testing.T) {
	if _, err := EChartsOptionFromSpecDir([]byte(`{"version":2,"mark":"pie"}`), ""); err == nil {
		t.Fatal("expected error for invalid spec")
	}
}

func TestOptionFromSpecDirReadsSourceAndOverlay(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "metrics.jsonl", `{"version":1,"name":"pi","time":"01","value":3}
{"version":1,"name":"pi","time":"02","value":7}
`)
	writeFile(t, dir, "events.jsonl", `{"version":1,"time":"02","title":"Launch"}
`)
	spec := `{"version":2,"mark":"line","title":"Sourced","data":{"kind":"metric","source":"metrics.jsonl"},
		"encoding":{"x":{"field":"time"},"y":[{"field":"value"}]},
		"overlays":[{"source":"events.jsonl","kind":"event","label":"title"}]}`
	out, err := EChartsOptionFromSpecDir([]byte(spec), dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"text":"Sourced"`, `"formatter":"Launch"`} {
		if !strings.Contains(out, want) {
			t.Errorf("option missing %q", want)
		}
	}
}

func TestOptionFromSpecInlineOverlayRecords(t *testing.T) {
	// Inline marker records travel with the spec, so an annotated chart renders even in the isolated
	// asset path (no data directory).
	spec := `{"version":2,"mark":"line","title":"Annotated","data":{"kind":"metric","records":[
		{"name":"pi","time":"01","value":3},{"name":"pi","time":"02","value":7}]},
		"encoding":{"x":{"field":"time"},"y":[{"field":"value"}]},
		"overlays":[{"records":[{"time":"02","title":"Launch"}],"kind":"event","label":"title"}]}`
	out, err := EChartsOptionFromSpecDir([]byte(spec), "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"formatter":"Launch"`) {
		t.Fatalf("inline overlay marker missing: %s", out)
	}
}

func TestOptionFromSpecDirAllowsLiteralOverlays(t *testing.T) {
	// Line/band overlays carry no source; with a data directory present they must not be read as one.
	dir := t.TempDir()
	writeFile(t, dir, "metrics.jsonl", `{"version":1,"name":"pi","time":"01","value":3}
{"version":1,"name":"pi","time":"02","value":7}
`)
	spec := `{"version":2,"mark":"line","data":{"kind":"metric","source":"metrics.jsonl"},
		"encoding":{"x":{"field":"time"},"y":[{"field":"value"}]},
		"overlays":[{"y":5,"label":"threshold"},{"from":"01","to":"02","label":"window"}]}`
	out, err := EChartsOptionFromSpecDir([]byte(spec), dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"formatter":"threshold"`, `"name":"window"`} {
		if !strings.Contains(out, want) {
			t.Errorf("option missing %q", want)
		}
	}
}

func TestOptionFromSpecDirConfinesSourceToDataDir(t *testing.T) {
	dir := t.TempDir()
	for _, source := range []string{"../secret.jsonl", "/etc/passwd"} {
		spec := `{"version":2,"mark":"line","data":{"kind":"metric","source":"` + source + `"},
			"encoding":{"x":{"field":"time"},"y":[{"field":"value"}]}}`
		_, err := EChartsOptionFromSpecDir([]byte(spec), dir)
		if err == nil || !strings.Contains(err.Error(), "inside the vault data directory") {
			t.Errorf("source %q: expected confinement error, got %v", source, err)
		}
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

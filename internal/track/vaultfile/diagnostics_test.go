package vaultfile

import (
	"container/list"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDiagnosticsHaveBoundedMemoryAndBurstOutput(t *testing.T) {
	// Package tests are serial; isolate the process-wide diagnostic cache and stderr sink.
	oldFiles, oldOrder := diagnostics.files, diagnostics.order
	oldWindow, oldEmitted, oldSummary := diagnostics.window, diagnostics.emitted, diagnostics.summarized
	diagnostics.files = make(map[string]*list.Element)
	diagnostics.order = list.New()
	diagnostics.window = time.Time{}
	diagnostics.emitted = 0
	diagnostics.summarized = false
	t.Cleanup(func() {
		diagnostics.files, diagnostics.order = oldFiles, oldOrder
		diagnostics.window, diagnostics.emitted, diagnostics.summarized = oldWindow, oldEmitted, oldSummary
	})
	f, err := os.CreateTemp(t.TempDir(), "diagnostics")
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stderr
	os.Stderr = f
	t.Cleanup(func() { os.Stderr = previous; f.Close() })
	for i := 0; i < diagnosticCacheLimit*4; i++ {
		Warn(fmt.Sprintf("fixture/%d.md", i), "invalid note metadata")
	}
	if len(diagnostics.files) > diagnosticCacheLimit || diagnostics.order.Len() > diagnosticCacheLimit {
		t.Fatal("unbounded diagnostic paths")
	}
	raw, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(raw), "\n"); got != diagnosticBurstLimit+1 {
		t.Fatalf("burst wrote %d lines", got)
	}
	if strings.Count(string(raw), "additional file diagnostics suppressed") != 1 {
		t.Fatal("missing bounded burst summary")
	}
	path := fmt.Sprintf("fixture/%d.md", diagnosticCacheLimit*4-1)
	size := len(raw)
	Warn(path, "invalid note metadata")
	raw, _ = os.ReadFile(f.Name())
	if len(raw) != size {
		t.Fatal("duplicate warning repeated")
	}
	Recovered(path)
	if _, ok := diagnostics.files[path]; ok {
		t.Fatal("recovered input retained")
	}
	diagnostics.window = time.Now().Add(-time.Minute)
	Warn(path, "invalid note metadata")
	raw, _ = os.ReadFile(f.Name())
	if len(raw) <= size {
		t.Fatal("new failure after recovery was not logged")
	}
}

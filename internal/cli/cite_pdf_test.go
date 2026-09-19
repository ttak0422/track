package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/fetch/pdf"
)

func TestCiteExtractedPDFPhysicalPages(t *testing.T) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("Poppler is not installed; nix develop supplies it")
	}
	for _, file := range []string{"pages.pdf", "single.pdf"} {
		t.Run(file, func(t *testing.T) {
			original, err := filepath.Abs("../fetch/pdf/testdata/" + file)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(original)
			if err != nil {
				t.Fatal(err)
			}
			extracted, err := pdf.Extract(context.Background(), raw)
			if err != nil {
				t.Fatal(err)
			}
			vault := t.TempDir()
			run := func(args ...string) map[string]any {
				t.Helper()
				out, code := runIn(t, vault, args...)
				if code != 0 {
					t.Fatalf("%v: %v", args, out)
				}
				return out
			}
			run("new", "--id", "100", "--title", "PDF", "--body", extracted.Text)
			saved := run("source", "save", "--id", "100", "--source", "https://example.test/document.pdf", "--format", "application/pdf", "--at", "2026-09-19T00:00:00Z", "--original", original)["record"].(map[string]any)
			version := saved["version"].(string)
			if saved["original_hash"] != extracted.OriginalHash {
				t.Fatal("saved different original bytes")
			}
			run("update", "--id", "100", "--body", "replacement working text")
			run("reindex", "--full")
			first := run("cite", "--id", "100", "--version", version, "--page", "1")
			if first["page"] != float64(1) {
				t.Fatal(first)
			}
			invalid := "2"
			if extracted.PageCount == 3 {
				blank := run("cite", "--id", "100", "--version", version, "--page", "2")
				if blank["body"] != "" {
					t.Fatalf("lost empty page: %v", blank)
				}
				third := run("cite", "--id", "100", "--version", version, "--page", "3")
				if !strings.Contains(third["body"].(string), "printed 1") {
					t.Fatalf("used printed page labels: %v", third)
				}
				invalid = "4"
			}
			if out, code := runIn(t, vault, "cite", "--id", "100", "--version", version, "--page", invalid); code != 1 {
				t.Fatalf("phantom final page: %v", out)
			}
		})
	}
}

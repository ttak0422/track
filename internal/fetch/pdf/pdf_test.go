package pdf

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestExtractPhysicalPages(t *testing.T) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("Poppler is not installed; nix develop supplies it")
	}
	for _, tc := range []struct {
		file     string
		pages    int
		empty    int
		contains string
	}{
		{"pages.pdf", 3, 1, "Results - printed 1"}, {"single.pdf", 1, 0, "One physical page"},
	} {
		t.Run(tc.file, func(t *testing.T) {
			raw, err := os.ReadFile("testdata/" + tc.file)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := Extract(context.Background(), raw)
			if err != nil {
				t.Fatal(err)
			}
			if doc.PageCount != tc.pages || len(doc.EmptyPages) != tc.empty || !strings.Contains(doc.Text, tc.contains) || !strings.HasSuffix(doc.Text, "\f") {
				t.Fatalf("unexpected document: %+v", doc)
			}
			if tc.empty == 1 && doc.EmptyPages[0] != 2 {
				t.Fatalf("lost physical blank page: %+v", doc)
			}
			if len(doc.ContentHash) != 64 || len(doc.OriginalHash) != 64 {
				t.Fatalf("missing hashes: %+v", doc)
			}
		})
	}
	blank, err := os.ReadFile("testdata/blank.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Extract(context.Background(), blank); err == nil || !strings.Contains(err.Error(), "no extractable text") {
		t.Fatalf("blank PDF was called readable: %v", err)
	}
	if _, err := Extract(context.Background(), []byte("%PDF-invalid")); err == nil {
		t.Fatal("accepted corrupt PDF")
	}
}

func TestExtractRejectsInvalidInputAndCancelledWork(t *testing.T) {
	if _, err := Extract(context.Background(), []byte("not a PDF")); err == nil {
		t.Fatal("accepted non-PDF")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	raw, err := os.ReadFile("testdata/single.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Extract(ctx, raw); err == nil {
		t.Fatal("ignored cancellation")
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := Extract(context.Background(), raw); err == nil || !strings.Contains(err.Error(), "install Poppler") {
		t.Fatalf("missing dependency: %v", err)
	}
}

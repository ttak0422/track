package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPDFCLI(t *testing.T) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("Poppler is not installed; nix develop supplies it")
	}
	file := "../../internal/fetch/pdf/testdata/pages.pdf"
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--file", file, "--at", "2026-09-19T09:00:00+09:00"}, &stdout, &stderr); code != 0 {
		t.Fatalf("%d: %s", code, stderr.String())
	}
	var record map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["page_count"] != float64(3) || record["time"] != "2026-09-19T00:00:00Z" || record["time_basis"] != "retrieved" || !strings.Contains(stderr.String(), "[2]") {
		t.Fatalf("record=%v stderr=%s", record, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"--note", file}, &stdout, &stderr); code != 0 {
		t.Fatalf("%d: %s", code, stderr.String())
	}
	if stdout.String() != record["text"] {
		t.Fatalf("note output changed evidence: %q", stdout.String())
	}
	original, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(t.TempDir(), "input.pdf")
	if err := os.WriteFile(local, original, 0644); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(filepath.Dir(local), "alias.pdf")
	if err := os.Link(local, alias); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"--file", local, "--out", local}, {"--file", local, "--out", alias},
		{"--file", file, "--at", "2026-09-19"}, {"--file", file, "--timeout", "0s"},
		{"--file", file, "another-file"}, {"--file", "missing.pdf"},
	} {
		stdout.Reset()
		stderr.Reset()
		if code := run(args, &stdout, &stderr); code == 0 || stdout.Len() != 0 {
			t.Fatalf("accepted %v: %s %s", args, stdout.String(), stderr.String())
		}
	}
	after, err := os.ReadFile(local)
	if err != nil || !bytes.Equal(after, original) {
		t.Fatal("overwrote input")
	}
	output := filepath.Join(t.TempDir(), "pages.txt")
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"--file", file, "--note", "--out", output}, &stdout, &stderr); code != 0 {
		t.Fatalf("%d: %s", code, stderr.String())
	}
	written, err := os.ReadFile(output)
	if err != nil || string(written) != record["text"] {
		t.Fatalf("output: %q %v", written, err)
	}
}

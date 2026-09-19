// Package pdf extracts local PDF text using Poppler, preserving physical page boundaries.
// It does not fetch remote documents, infer publication dates, or perform OCR.
package pdf

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

type Document struct {
	Text         string `json:"text"`
	PageCount    int    `json:"page_count"`
	EmptyPages   []int  `json:"empty_pages"`
	OriginalHash string `json:"original_hash"`
	ContentHash  string `json:"content_hash"`
}

// Extract copies the supplied bytes into a private temporary directory so hashes and extraction
// always describe the same input, even when the caller's original file changes concurrently.
func Extract(ctx context.Context, original []byte) (Document, error) {
	if !bytes.HasPrefix(original, []byte("%PDF-")) {
		return Document{}, fmt.Errorf("input is not a PDF")
	}
	dir, err := os.MkdirTemp("", "track-pdf-")
	if err != nil {
		return Document{}, err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "input.pdf")
	if err := os.WriteFile(path, original, 0600); err != nil {
		return Document{}, err
	}
	cmd := exec.CommandContext(ctx, "pdftotext", "-enc", "UTF-8", "-eol", "unix", "-layout", path, "-")
	var diagnostics bytes.Buffer
	cmd.Stderr = &diagnostics
	output, err := cmd.Output()
	if ctx.Err() != nil {
		return Document{}, fmt.Errorf("PDF extraction: %w", ctx.Err())
	}
	if err != nil {
		return Document{}, fmt.Errorf("pdftotext failed (install Poppler): %w: %s", err, strings.TrimSpace(diagnostics.String()))
	}
	// Warnings can indicate skipped/corrupt content. Do not publish a partial extraction as success.
	if diagnostics.Len() != 0 {
		return Document{}, fmt.Errorf("pdftotext reported incomplete or suspect input: %s", strings.TrimSpace(diagnostics.String()))
	}
	if !utf8.Valid(output) || !bytes.HasSuffix(output, []byte("\f")) {
		return Document{}, fmt.Errorf("pdftotext did not return UTF-8 text with page terminators")
	}
	text := string(output)
	pages := strings.Split(strings.TrimSuffix(text, "\f"), "\f")
	doc := Document{Text: text, PageCount: len(pages), EmptyPages: []int{}, OriginalHash: fmt.Sprintf("%x", sha256.Sum256(original)), ContentHash: fmt.Sprintf("%x", sha256.Sum256(output))}
	for i, page := range pages {
		if strings.TrimSpace(page) == "" {
			doc.EmptyPages = append(doc.EmptyPages, i+1)
		}
	}
	if len(doc.EmptyPages) == doc.PageCount {
		return Document{}, fmt.Errorf("PDF has no extractable text; OCR may be required")
	}
	return doc, nil
}

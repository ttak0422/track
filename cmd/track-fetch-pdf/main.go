// track-fetch-pdf converts a local PDF into event JSONL or exact, page-delimited note text.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/fetch/pdf"
	"github.com/ttak0422/track/internal/track/dataset"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("track-fetch-pdf", flag.ContinueOnError)
	fs.SetOutput(stderr)
	file := fs.String("file", "", "local PDF file (or one positional path); remote URLs are not fetched")
	title := fs.String("title", "", "document title; defaults to filename")
	source := fs.String("source", "", "original source location; defaults to local file URL")
	at := fs.String("at", "", "retrieval time with timezone (RFC3339); defaults to now, never publication time")
	note := fs.Bool("note", false, "print exact extracted text, including physical page terminators, instead of JSONL")
	out := fs.String("out", "", "write output to this file and print a JSON summary")
	timeout := fs.Duration("timeout", 30*time.Second, "PDF extraction timeout")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	fail := func(err error) int { fmt.Fprintf(stderr, "track-fetch-pdf: %v\n", err); return 1 }
	if *file == "" && fs.NArg() == 1 {
		*file = fs.Arg(0)
	} else if fs.NArg() != 0 {
		return fail(fmt.Errorf("provide one local PDF file"))
	}
	if *file == "" || *timeout <= 0 {
		return fail(fmt.Errorf("a local PDF file and positive timeout are required"))
	}
	stamp := time.Now().UTC()
	if *at != "" {
		parsed, err := time.Parse(time.RFC3339Nano, *at)
		if err != nil {
			return fail(fmt.Errorf("invalid retrieval time: %w", err))
		}
		stamp = parsed.UTC()
	}
	path, err := filepath.Abs(*file)
	if err != nil {
		return fail(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		return fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	doc, err := pdf.Extract(ctx, original)
	if err != nil {
		return fail(err)
	}
	if strings.TrimSpace(*title) == "" {
		*title = filepath.Base(path)
	}
	if *source == "" {
		*source = (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
	}
	rec := dataset.Record{
		"version": dataset.SchemaVersion, "title": *title, "time": stamp.Format(time.RFC3339Nano),
		"time_basis": "retrieved", "url": *source, "format": "application/pdf",
		"text": doc.Text, "page_count": doc.PageCount, "empty_pages": doc.EmptyPages,
		"original_hash": doc.OriginalHash, "content_hash": doc.ContentHash,
	}
	if err := dataset.Validate(dataset.KindEvent, rec); err != nil {
		return fail(err)
	}
	if len(doc.EmptyPages) > 0 {
		fmt.Fprintf(stderr, "track-fetch-pdf: pages without extractable text: %v (blank or OCR required)\n", doc.EmptyPages)
	}
	payload, err := json.Marshal(rec)
	if err != nil {
		return fail(err)
	}
	payload = append(payload, '\n')
	if *note {
		payload = []byte(doc.Text)
	}
	if *out != "" {
		// Prevent a mistaken --out from replacing the input PDF, including symlink/hardlink aliases.
		inputInfo, err := os.Stat(path)
		if err != nil {
			return fail(err)
		}
		if outputInfo, err := os.Stat(*out); err == nil && os.SameFile(inputInfo, outputInfo) {
			return fail(fmt.Errorf("output would overwrite the input PDF"))
		}
		if err := os.WriteFile(*out, payload, 0644); err != nil {
			return fail(err)
		}
		if err := json.NewEncoder(stdout).Encode(map[string]any{"path": *out, "records": 1, "page_count": doc.PageCount, "empty_pages": doc.EmptyPages, "original_hash": doc.OriginalHash}); err != nil {
			return fail(err)
		}
		return 0
	}
	if _, err := stdout.Write(payload); err != nil {
		return fail(err)
	}
	return 0
}

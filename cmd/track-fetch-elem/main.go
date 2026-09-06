// track-fetch-elem clips one browser-selected web element into Canonical Data Model event JSONL — a
// track-fetch-* tool (see docs/spec/fetch.md for the contract). It reads a grab payload (produced by
// an external browser tool) from stdin or a file, clamps it to budget, allowlists attributes,
// redacts secrets, sanitizes URLs, and emits one event record carrying the element rendered as
// Markdown. It never touches the network or the vault: track never runs a browser, and this tool
// only converts the payload the browser already captured.
//
// Usage:
//
//	track-fetch-elem [--in <file>] [--out <file>] [--note]
//
// With --note the tool prints a ready-to-pipe Markdown note body instead of JSONL, so a clipped
// element goes straight into a note:
//
//	<grab-tool> | track-fetch-elem --note | track new --title "A heading"
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/ttak0422/track/internal/fetch/elem"
	"github.com/ttak0422/track/internal/track/dataset"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("track-fetch-elem", flag.ContinueOnError)
	fs.SetOutput(stderr)
	in := fs.String("in", "", "read the grab payload from this file instead of stdin")
	out := fs.String("out", "", "write JSONL to this file instead of stdout (prints a JSON summary)")
	note := fs.Bool("note", false, "print a ready-to-pipe Markdown note body instead of JSONL")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(stderr, "track-fetch-elem: no positional arguments; pass the payload via stdin or --in")
		fs.Usage()
		return 2
	}

	var raw []byte
	var err error
	if *in != "" {
		raw, err = os.ReadFile(*in)
	} else {
		raw, err = io.ReadAll(io.LimitReader(stdin, 20<<20))
	}
	if err != nil {
		return fail(stderr, err)
	}

	el, err := elem.Clamp(raw, elem.DefaultBudget)
	if err != nil {
		return fail(stderr, err)
	}

	if *note {
		fmt.Fprint(stdout, elem.NoteBody(el))
		return 0
	}

	rec, err := elem.Record(el, time.Now())
	if err != nil {
		return fail(stderr, err)
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return fail(stderr, err)
	}
	jsonl := string(line) + "\n"

	if *out == "" {
		fmt.Fprint(stdout, jsonl)
		return 0
	}
	if err := os.WriteFile(*out, []byte(jsonl), 0o644); err != nil {
		return fail(stderr, err)
	}
	summary, _ := json.Marshal(map[string]any{
		"path": *out, "kind": string(dataset.KindEvent), "records": 1, "title": rec["title"],
	})
	fmt.Fprintln(stdout, string(summary))
	return 0
}

func fail(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "track-fetch-elem: %v\n", err)
	return 1
}

package cli

import (
	"flag"
	"fmt"
	"strconv"
	"strings"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/provenance"
)

type sourceInputs []provenance.Reference

func (f *sourceInputs) String() string { return fmt.Sprint([]provenance.Reference(*f)) }
func (f *sourceInputs) Set(value string) error {
	id, version, ok := strings.Cut(value, ":")
	n, err := strconv.ParseInt(id, 10, 64)
	if !ok || err != nil || n <= 0 || version == "" {
		return fmt.Errorf("input must be NOTE_ID:VERSION")
	}
	*f = append(*f, provenance.Reference{NoteID: n, Version: version})
	return nil
}

func cmdSource(args []string) int {
	if len(args) == 0 {
		return fail("usage: track source <save|list> ...")
	}
	sub := args[0]
	if sub != "save" && sub != "list" {
		return fail("unknown source command %q", sub)
	}
	fs := flag.NewFlagSet("source "+sub, flag.ContinueOnError)
	id := fs.Int64("id", 0, "existing note id (stable across renames)")
	var opts provenance.Options
	var inputs sourceInputs
	if sub == "save" {
		fs.StringVar(&opts.Source, "source", "", "original location / source identity; exclusive with --input")
		fs.StringVar(&opts.Format, "format", "", "source media type, e.g. text/html or application/pdf")
		fs.StringVar(&opts.At, "at", "", "retrieval/generation time with timezone (RFC3339); never inferred publication time")
		fs.StringVar(&opts.Original, "original", "", "optional original local file to preserve with this version")
		fs.Var(&inputs, "input", "input NOTE_ID:VERSION (repeatable); records a derived artifact")
		fs.StringVar(&opts.Method, "method", "", "processing method/model identifier for a derived artifact")
		fs.StringVar(&opts.Settings, "settings", "", "exact configuration identifier for a derived artifact")
		fs.StringVar(&opts.Run, "run", "", "explicit regeneration key; reuse it when retrying the same run")
	}
	if code, ok := parseArgs(fs, args[1:]); !ok {
		return code
	}
	if fs.NArg() != 0 {
		return fail("unexpected positional arguments")
	}
	cfg, err := config.Load()
	if err == nil {
		err = requireVaultDir(cfg)
	}
	if err != nil {
		return fail("%v", err)
	}
	if sub == "list" {
		records, err := provenance.List(cfg, *id)
		if err != nil {
			return fail("%v", err)
		}
		return emit(map[string]any{"versions": records})
	}
	opts.Inputs = inputs
	r, created, err := provenance.Save(cfg, *id, opts)
	if err != nil {
		return fail("%v", err)
	}
	return emit(map[string]any{"record": r, "created": created})
}

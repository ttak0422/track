package cli

import (
	"crypto/sha256"
	"flag"
	"fmt"
	"unicode/utf8"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/provenance"
)

func cmdCite(args []string) int {
	fs := flag.NewFlagSet("cite", flag.ContinueOnError)
	id := fs.Int64("id", 0, "note id (stable across renames)")
	version := fs.String("version", "", "saved source version; omitted selects the mutable working body")
	var position provenance.Position
	fs.StringVar(&position.Heading, "heading", "", "exact heading text; duplicates are errors")
	fs.IntVar(&position.Level, "level", 0, "heading level 1..6 (requires --heading)")
	fs.StringVar(&position.Block, "block", "", "manual block id without ^")
	fs.IntVar(&position.Page, "page", 0, "physical page, 1-based; requires saved form-feed boundaries")
	fs.IntVar(&position.StartLine, "start-line", 0, "first line, 1-based inclusive; requires --end-line")
	fs.IntVar(&position.EndLine, "end-line", 0, "last line, 1-based inclusive; requires --start-line")
	if code, ok := parseArgs(fs, args); !ok {
		return code
	}
	if fs.NArg() != 0 {
		return fail("unexpected positional arguments")
	}
	for _, name := range []string{"version", "heading", "block"} {
		if flagWasSet(fs, name) && fs.Lookup(name).Value.String() == "" {
			return fail("--%s cannot be empty", name)
		}
	}
	for _, name := range []string{"level", "page", "start-line", "end-line"} {
		if flagWasSet(fs, name) && fs.Lookup(name).Value.String() == "0" {
			return fail("--%s must be positive", name)
		}
	}
	cfg, err := config.Load()
	if err == nil {
		err = requireVaultDir(cfg)
	}
	if err != nil {
		return fail("%v", err)
	}
	n, err := provenance.Current(cfg, *id)
	if err != nil {
		return fail("%v", err)
	}
	body := n.Body
	if *version != "" {
		record, err := provenance.Read(cfg, provenance.Reference{NoteID: *id, Version: *version})
		if err != nil {
			return fail("%v", err)
		}
		body = record.Body
	}
	if !utf8.ValidString(body) {
		return fail("evidence body must be UTF-8")
	}
	selection, err := provenance.Select(body, position)
	if err != nil {
		return fail("%v", err)
	}
	return emit(struct {
		provenance.Reference
		provenance.Selection
		Title       string `json:"title"`
		ContentHash string `json:"content_hash"`
		Pinned      bool   `json:"pinned"`
	}{Reference: provenance.Reference{NoteID: *id, Version: *version}, Selection: selection, Title: n.Meta.Title, ContentHash: fmt.Sprintf("%x", sha256.Sum256([]byte(body))), Pinned: *version != ""})
}

package cli

import (
	"flag"
	"os"
	"strings"

	"github.com/ttak0422/track/internal/track/asset"
	"github.com/ttak0422/track/internal/track/config"
)

// cmdAsset routes the asset subcommands. Assets are per-kind media stored under note/assets and
// journal/assets; they are not indexed notes, so these commands only touch config and the filesystem.
func cmdAsset(args []string) int {
	if len(args) == 0 {
		return fail("usage: track asset <import|dir> ...")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "import":
		return cmdAssetImport(rest)
	case "dir":
		return cmdAssetDir(rest)
	default:
		return fail("unknown asset command %q", sub)
	}
}

// cmdAssetImport copies a local file into a kind's assets directory and reports the path plus the
// "assets/<file>" reference to embed from a note of that kind.
func cmdAssetImport(args []string) int {
	fs := flag.NewFlagSet("asset import", flag.ContinueOnError)
	kind := fs.String("kind", config.KindNote, "note kind the asset belongs to: note or journal")
	file := fs.String("file", "", "path to the file to import (or pass it as the first argument)")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}
	src := strings.TrimSpace(*file)
	if src == "" && fs.NArg() > 0 {
		src = strings.TrimSpace(fs.Arg(0))
	}
	if src == "" {
		return fail("--file (or a path argument) is required")
	}

	cfg, err := config.Load()
	if err != nil {
		return fail("%v", err)
	}
	k := normalizeAssetKind(*kind)
	stored, err := asset.Import(cfg, k, src)
	if err != nil {
		return fail("%v", err)
	}
	return emit(map[string]any{"kind": k, "path": stored.Path, "ref": stored.Ref, "name": stored.Name})
}

// cmdAssetDir reports the assets directory for a kind, optionally creating it.
func cmdAssetDir(args []string) int {
	fs := flag.NewFlagSet("asset dir", flag.ContinueOnError)
	kind := fs.String("kind", config.KindNote, "note kind: note or journal")
	ensure := fs.Bool("ensure", false, "create the directory if it does not exist")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		return fail("%v", err)
	}
	k := normalizeAssetKind(*kind)
	dir := cfg.AssetsDirForKind(k)
	if *ensure {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fail("create assets dir: %v", err)
		}
	}
	return emit(map[string]any{"kind": k, "dir": dir})
}

// normalizeAssetKind restricts the kind to the two that have assets directories, defaulting to note.
func normalizeAssetKind(kind string) string {
	if strings.TrimSpace(kind) == config.KindJournal {
		return config.KindJournal
	}
	return config.KindNote
}

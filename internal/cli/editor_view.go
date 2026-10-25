package cli

import (
	"flag"
	"fmt"
	"strings"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/editorview"
	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/link"
	"github.com/ttak0422/track/internal/track/note"
)

type editorResult struct {
	editorview.Entry
	WikiLink string `json:"wikilink"`
}

func editorRow(cfg *config.Config, e editorview.Entry) editorResult {
	return editorResult{Entry: e, WikiLink: "[[" + link.NoteTarget(cfg.LinkFormat, e.ID, e.Title) + "]]"}
}
func cmdEditorPath(args []string) int {
	fs := flag.NewFlagSet("editor-path", flag.ContinueOnError)
	path := fs.String("path", "", "canonical note or approved editor alias")
	sourcePath := fs.String("source-path", "", "optional source note: use its link format and cross-vault namespace")
	check := fs.Bool("check", false, "validate only; never create or repair an alias")
	if code, ok := parseArgs(fs, args); !ok {
		return code
	}
	if *path == "" {
		return fail("--path is required")
	}
	cfg, err := config.Load()
	if err != nil {
		return fail("%v", err)
	}
	if editorview.IsViewPath(*path) {
		aliasCfg, _, err := editorview.Discover(*path)
		if err != nil {
			return fail("%v", err)
		}
		if selectedVault != "" && aliasCfg.VaultDir != cfg.VaultDir {
			return fail("editor alias belongs to a different vault")
		}
		cfg = aliasCfg
	}
	e, err := editorview.Resolve(cfg, *path)
	if err != nil {
		return fail("%v", err)
	}
	meta, _, err := note.ReadMetadata(cfg.MetadataPath(e.ID))
	if err != nil {
		return fail("%v", err)
	}
	e.Title = meta.Title
	if !*check {
		e, err = editorview.Ensure(cfg, e.FileKind, e.ID, e.Title)
		if err != nil {
			return fail("%v", err)
		}
	}
	row := editorRow(cfg, e)
	if *sourcePath != "" {
		sourceCfg := cfg
		if editorview.IsViewPath(*sourcePath) {
			discovered, _, err := editorview.Discover(*sourcePath)
			if err != nil {
				return fail("%v", err)
			}
			sourceCfg = discovered
		} else if root, ok := vaultRootOf(*sourcePath); ok {
			discovered, err := config.LoadAt(root)
			if err != nil {
				return fail("%v", err)
			}
			sourceCfg = discovered
		}
		if _, err := editorview.Resolve(sourceCfg, *sourcePath); err != nil {
			return fail("invalid source note: %v", err)
		}
		target := link.NoteTarget(sourceCfg.LinkFormat, e.ID, e.Title)
		if sourceCfg.VaultDir != cfg.VaultDir {
			name := ""
			for candidate, path := range sourceCfg.Vaults {
				canonical, err := config.CanonicalPath(path)
				if err == nil && canonical == cfg.VaultDir {
					name = candidate
					break
				}
			}
			if name == "" {
				return fail("target vault is not registered in the source note's namespace")
			}
			target = name + ":" + target
		}
		row.WikiLink = "[[" + target + "]]"
	}
	return emit(row)
}
func cmdEditorView(args []string) int {
	fs := flag.NewFlagSet("editor-view", flag.ContinueOnError)
	if code, ok := parseArgs(fs, args); !ok {
		return code
	}
	if fs.NArg() != 0 {
		return fail("editor-view takes no positional arguments")
	}
	if !editorview.Enabled() {
		return fail("set TRACK_EDITOR_VIEW to an absolute nonsynced machine-local directory first")
	}
	cfg, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()
	if _, err = index.New(cfg, s).RefreshIfStale(); err != nil {
		return fail("%v", err)
	}
	refs, err := s.AllNotes()
	if err != nil {
		return fail("%v", err)
	}
	root, err := editorview.Initialize(cfg)
	if err != nil {
		return fail("%v", err)
	}
	rows := make([]editorResult, 0, len(refs))
	for _, ref := range refs {
		if ref.FileKind != config.KindNote && ref.FileKind != config.KindJournal {
			continue
		}
		e, err := editorview.Ensure(cfg, ref.FileKind, ref.NoteID, ref.Title)
		if err != nil {
			return fail("%v", err)
		}
		rows = append(rows, editorRow(cfg, e))
	}
	return emit(map[string]any{"root": root, "entries": rows})
}

// canonicalEditorArgs translates only approved aliases, before ordinary CLI ID parsing. Canonical
// automation output remains untouched; editor-path keeps its original argument to validate writes.
func canonicalEditorArgs(args []string) ([]string, error) {
	if len(args) == 0 || args[0] == "editor-path" {
		return args, nil
	}
	out := append([]string(nil), args...)
	for i := 1; i < len(out); i++ {
		for _, flag := range []string{"--path", "--parent-path"} {
			pos := i
			value := ""
			inline := false
			if out[i] == flag && i+1 < len(out) {
				pos = i + 1
				value = out[pos]
			} else if strings.HasPrefix(out[i], flag+"=") {
				value = strings.TrimPrefix(out[i], flag+"=")
				inline = true
			} else {
				continue
			}
			if editorview.IsViewPath(value) {
				cfg, e, err := editorview.Discover(value)
				if err != nil {
					return nil, err
				}
				if selectedVault != "" {
					active, err := config.Load()
					if err != nil {
						return nil, err
					}
					if active.VaultDir != cfg.VaultDir {
						return nil, fmt.Errorf("editor alias belongs to a different vault")
					}
				}
				if inline {
					out[i] = flag + "=" + e.Path
				} else {
					out[pos] = e.Path
				}
			}
		}
	}
	return out, nil
}

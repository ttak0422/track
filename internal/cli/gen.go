package cli

import (
	"flag"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/gen"
	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

// cmdGen dispatches the generation-management subcommands (see internal/track/gen for the model).
func cmdGen(args []string) int {
	if len(args) == 0 {
		return fail("gen: subcommand required (increment|undo|redo|list|peek)")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "increment":
		return cmdGenIncrement(rest)
	case "undo":
		return cmdGenMove(rest, "undo")
	case "redo":
		return cmdGenMove(rest, "redo")
	case "list":
		return cmdGenList(rest)
	case "peek":
		return cmdGenPeek(rest)
	default:
		return fail("gen: unknown subcommand %q (increment|undo|redo|list|peek)", sub)
	}
}

func cmdGenIncrement(args []string) int {
	fs := flag.NewFlagSet("gen increment", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		return fail("%v", err)
	}
	res, err := gen.New(cfg).Increment()
	if err != nil {
		return fail("gen increment: %v", err)
	}
	return emit(res)
}

// cmdGenMove runs undo or redo, then rebuilds the index from scratch: the restore replaced note
// files and sidecars wholesale, so a full reset is the honest way to make search and links match.
func cmdGenMove(args []string, dir string) int {
	fs := flag.NewFlagSet("gen "+dir, flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		return fail("%v", err)
	}
	m := gen.New(cfg)
	var res gen.MoveResult
	if dir == "undo" {
		res, err = m.Undo()
	} else {
		res, err = m.Redo()
	}
	if err != nil {
		return fail("gen %s: %v", dir, err)
	}
	if err := store.Reset(cfg.DBPath); err != nil {
		return fail("reset index db: %v", err)
	}
	s, err := store.Open(cfg.DBPath)
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()
	rep, err := index.New(cfg, s).Full()
	if err != nil {
		return fail("reindex: %v", err)
	}
	out := map[string]any{"gen": res.Gen, "reindexed": rep.Indexed}
	if res.Saved != 0 {
		out["saved"] = res.Saved
	}
	return emit(out)
}

func cmdGenList(args []string) int {
	fs := flag.NewFlagSet("gen list", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		return fail("%v", err)
	}
	res, err := gen.New(cfg).List()
	if err != nil {
		return fail("gen list: %v", err)
	}
	return emit(res)
}

// cmdGenPeek prints a note's content as of a generation to stdout, like export. Reading old
// content without moving the cursor is what makes selective revert possible: diff it against the
// current note and write back only the parts to restore with `track update`.
func cmdGenPeek(args []string) int {
	fs := flag.NewFlagSet("gen peek", flag.ContinueOnError)
	genNum := fs.Int("gen", 0, "generation number (default: cursor generation)")
	id := fs.Int64("id", 0, "note id")
	title := fs.String("title", "", "note title (alternative to --id)")
	path := fs.String("path", "", "note path (alternative to --id)")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}
	cfg, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	rel, err := peekRelPath(cfg, s, *genNum, *id, strings.TrimSpace(*title), strings.TrimSpace(*path))
	if err != nil {
		return fail("%v", err)
	}
	content, err := gen.New(cfg).Peek(*genNum, rel)
	if err != nil {
		return fail("gen peek: %v", err)
	}
	fmt.Print(content)
	return 0
}

// peekRelPath resolves --id/--title/--path to a vault-relative note path without requiring the
// file to exist in the working vault — peek's target may be a note that only lives in a snapshot.
// A title only resolves while the note is still indexed; use --id for deleted notes.
func peekRelPath(cfg *config.Config, s *store.Store, genNum int, id int64, title, path string) (string, error) {
	abs := ""
	switch {
	case path != "":
		kind, ok := cfg.KindFromPath(path)
		if !ok {
			return "", fmt.Errorf("path is not a vault note: %s", path)
		}
		noteID, err := note.IDFromPath(path)
		if err != nil {
			return "", err
		}
		abs = cfg.PathForKind(kind, noteID)
	case title != "":
		ref, found, err := s.ResolveTerm(title)
		if err != nil {
			return "", fmt.Errorf("resolve: %v", err)
		}
		if !found {
			return "", fmt.Errorf("no note titled %q (deleted notes resolve by --id only)", title)
		}
		abs = cfg.PathForKind(ref.FileKind, ref.NoteID)
	case id != 0:
		// A bare id does not carry its kind; probe the note layout first, then the journal one,
		// against the snapshots rather than the working tree.
		notePath := cfg.NotePath(id)
		relNote, err := filepath.Rel(cfg.VaultDir, notePath)
		if err != nil {
			return "", err
		}
		if _, peekErr := gen.New(cfg).Peek(genNum, filepath.ToSlash(relNote)); peekErr == nil {
			return filepath.ToSlash(relNote), nil
		}
		abs = cfg.JournalPath(fmt.Sprintf("%d", id))
	default:
		return "", fmt.Errorf("--id, --title, or --path is required")
	}
	rel, err := filepath.Rel(cfg.VaultDir, abs)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

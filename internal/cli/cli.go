// Package cli is the command router for the track binary.
// It is a thin layer over the engine packages (config, note, store, index): it parses arguments, calls engine functions, and prints JSON.
// A future LSP server reuses the same engine packages directly rather than shelling out to these commands.
package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/store"
)

const Version = "0.1.0"

// Run dispatches a subcommand and returns a process exit code.
func Run(args []string) int {
	if len(args) == 0 {
		usage()
		return 1
	}

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "version", "--version", "-v":
		fmt.Printf("track %s\n", Version)
		return 0
	case "dump":
		fmt.Printf("{\n  \"version\": %q,\n  \"entries\": []\n}\n", Version)
		return 0
	case "reindex":
		return cmdReindex(rest)
	case "new":
		return cmdNew(rest)
	case "open":
		return cmdOpen(rest)
	case "journal":
		return cmdJournal(rest)
	case "keywords":
		return cmdKeywords(rest)
	case "resolve":
		return cmdResolve(rest)
	case "search":
		return cmdSearch(rest)
	case "backlinks":
		return cmdBacklinks(rest)
	case "graph":
		return cmdGraph(rest)
	case "template":
		return cmdTemplate(rest)
	case "babel":
		return cmdBabel(rest)
	case "export":
		return cmdExport(rest)
	default:
		fmt.Fprintf(os.Stderr, "track: unknown command %q\n", cmd)
		usage()
		return 1
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `track - note tool

Usage:
  track new --title <t> [--id <id>] [--template <s>]
                                        create a note (fails if the title exists)
  track open --title <t> [--template <s>]
                                        open the note with this title, creating it if absent
  track journal [--offset <n>] [--template <s>]
                                        open/create a daily note
  track reindex [--full]                rebuild the index
  track keywords                        dump the auto-link dictionary (JSON)
  track resolve --term <s>              resolve a keyword to a note (JSON)
  track search --query <s> [--scope all|title|body] [--limit N]
                                        search notes (JSON)
  track backlinks (--id N | --path P)   list backlinks (JSON)
  track graph (--id N | --path P)       show a local link graph (JSON)
  track template new --name <s> [--id N]
                                        create a template (JSON)
  track template open --name <s>         open or create a template (JSON)
  track template list                    list templates (JSON)
  track babel exec (--id N | --path P) [--name S | --ordinal N] [--yes]
                                        run a source block (JSON)
  track babel restore (--id N | --path P)
                                        list stored source block results (JSON)
  track export (--id N | --title S | --path P) [--out F] [--frontmatter] [--exports-default M]
                                        write a note out as Markdown (stdout, or JSON path with --out)
  track dump                            print placeholder state (JSON)
  track version                         print the version
`)
}

// emit prints v as a single line of compact JSON to stdout.
func emit(v any) int {
	b, err := json.Marshal(v)
	if err != nil {
		return fail("marshal: %v", err)
	}
	fmt.Println(string(b))
	return 0
}

// fail prints {"error":...} to stdout and returns exit code 1, so the Lua side can branch on decoded.error uniformly.
func fail(format string, args ...any) int {
	msg := fmt.Sprintf(format, args...)
	b, _ := json.Marshal(map[string]string{"error": msg})
	fmt.Println(string(b))
	return 1
}

// open loads config and opens the index store.
func open() (*config.Config, *store.Store, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, err
	}
	s, err := store.Open(cfg.DBPath)
	if err != nil {
		return nil, nil, err
	}
	return cfg, s, nil
}

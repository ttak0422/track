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
	case "append":
		return cmdAppend(rest)
	case "rename":
		return cmdRename(rest)
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
	case "web":
		return cmdWeb(rest)
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

Notes carry content through these commands; titles are link keywords. Write [[Title]] in a body to
link notes. --body is read from stdin when omitted and stdin is piped. --ai stamps the reserved
generated-by-ai tag. Creating or appending indexes the note immediately, so reindex is for bulk repair.

Usage:
  track new --title <t> [--id <id>] [--template <s>] [--body <s>] [--tag <s>] [--ai]
                                        create a note (fails if the title exists); --body is saved verbatim
  track open --title <t> [--template <s>] [--body <s>] [--tag <s>] [--ai]
                                        open the note with this title, creating it if absent
  track append (--id N | --title S | --path P) [--body <s>] [--tag <s>] [--ai]
                                        append body text and/or merge tags into an existing note
  track rename (--id N | --title S | --path P) --to S
                                        rename a note's title and rewrite its backlinks (JSON)
  track journal [--offset <n>] [--template <s>] [--body <s>] [--ai]
                                        open/create a daily note
  track reindex [--full]                rebuild the index
  track keywords                        dump the auto-link dictionary (JSON)
  track resolve --term <s>              resolve a keyword to a note (JSON)
  track search --query <s> [--scope all|title|body] [--limit N]
                                        search notes (JSON)
  track backlinks (--id N | --path P)   list backlinks (JSON)
  track graph (--id N | --path P)       show a local link graph (JSON)
  track web [--addr 127.0.0.1:8765]      serve the local web workspace
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

Examples:
  cat article.md | track new --title "記事" --ai
                                        save stdin verbatim; leading # headings are allowed
  printf '本文 [[他ノート]]\n' | track open --title "メモ"
                                        create if absent, otherwise open existing note
  track search --query '#zettel'         filter search by #tag
  track export --id 1781314534000        write a note as Markdown to stdout
  track rename --title "旧題" --to "新題"
                                        rename title and rewrite backlinks
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

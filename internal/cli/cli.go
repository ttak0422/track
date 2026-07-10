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
	case "init":
		return cmdInit(rest)
	case "reindex":
		return cmdReindex(rest)
	case "doctor":
		return cmdDoctor(rest)
	case "refresh-all":
		return cmdRefreshAll(rest)
	case "new":
		return cmdNew(rest)
	case "open":
		return cmdOpen(rest)
	case "journal":
		return cmdJournal(rest)
	case "append":
		return cmdAppend(rest)
	case "update":
		return cmdUpdate(rest)
	case "meta":
		return cmdMeta(rest)
	case "toggle":
		return cmdToggle(rest)
	case "asset":
		return cmdAsset(rest)
	case "rename":
		return cmdRename(rest)
	case "rm":
		return cmdRm(rest)
	case "gen":
		return cmdGen(rest)
	case "keywords":
		return cmdKeywords(rest)
	case "resolve":
		return cmdResolve(rest)
	case "search":
		return cmdSearch(rest)
	case "notes":
		return cmdNotes(rest)
	case "backlinks":
		return cmdBacklinks(rest)
	case "agenda":
		return cmdAgenda(rest)
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
	case "export-site":
		return cmdExportSite(rest)
	case "render":
		return cmdRender(rest)
	default:
		fmt.Fprintf(os.Stderr, "track: unknown command %q\n", cmd)
		usage()
		return 1
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `track - note tool

Notes carry content through these commands; titles are link keywords. Write [[Title]] in a body to
link notes. --body is read from stdin when omitted and stdin is piped. Creating or appending indexes
the note immediately, so reindex is for bulk repair.

Usage:
  track new --title <t> [--id <id>] [--template <s>] [--body <s>] [--tag <s>]
                                        create a note (fails if the title exists); --body is saved verbatim
  track open --title <t> [--template <s>] [--body <s>] [--tag <s>]
                                        open the note with this title, creating it if absent
  track append (--id N | --title S | --path P) [--body <s>] [--tag <s>]
                                        append body text and/or merge tags into an existing note
  track update (--id N | --title S | --path P) [--body <s>] [--tag <s>] [--clear-tags]
                                        replace body text and/or update tags on an existing note
  track meta (--id N | --title S | --path P) [--description S] [--image assets/F]
                                        print a note's page metadata, or set it: description (og:description)
                                        and cover image (og:image; an existing vault asset). An empty
                                        value clears the field (JSON)
  track toggle (--id N | --title S | --path P) --line N [--state toggle|check|uncheck]
                                        flip (or set) a task checkbox on one line of a note (JSON)
  track asset import <file>             copy a file into the vault's assets/ dir; prints the assets/<file> ref (JSON)
  track asset dir [--ensure]            print (and optionally create) the vault's assets directory (JSON)
  track rename (--id N | --title S | --path P) --to S
                                        rename a note's title and rewrite its backlinks (JSON)
  track rm (--id N | --title S | --path P)
                                        soft-delete a note: move it and its sidecar into .track/trash (JSON)
  track gen increment                   save the working vault as a new generation; drops generations
                                        past the cursor and prunes old ones beyond gen_keep (JSON)
  track gen undo                        step back one generation and restore it; unsaved changes are
                                        auto-saved as a generation first when at the head (JSON)
  track gen redo                        step forward one generation and restore it (JSON)
  track gen list                        list generations, the cursor, and dirty state (JSON)
  track gen peek [--gen N] (--id N | --title S | --path P)
                                        print a note's content as of a generation (default: cursor)
  track journal [--offset <n>] [--template <s>] [--body <s>]
                                        open/create a daily note
  track init                            create the vault directory skeleton (idempotent, JSON)
  track reindex [--full]                rebuild the index
  track doctor [--fix]                  report vault/sidecar divergence without changing files (JSON);
                                        --fix repairs it by auto-numbered restore, then reindexes
  track refresh-all                     run the maintenance pipeline in one idempotent pass (full reindex +
                                        read-only doctor report); suitable for cron/launchd (JSON)
  track keywords                        dump the auto-link dictionary (JSON)
  track resolve (--term <s> | <s>)      resolve a keyword to a note (JSON)
  track search --query <s> [--scope all|title|body] [--limit N]
                                        search notes (JSON)
  track notes [--untagged] [--limit N]  list notes, newest first; --untagged keeps only notes with no
                                        tags, for a curation pass that adds tags via track append --tag (JSON)
  track backlinks (--id N | --path P)   list backlinks (JSON)
  track agenda [--date YYYY-MM-DD]       list notes active on a day (JSON)
  track graph (--id N | --path P)       show a local link graph (JSON)
  track web [--addr 127.0.0.1:8765]      serve the local web workspace
  track template new --name <s> [--id N]
                                        create a template (JSON)
  track template open --name <s>         open or create a template (JSON)
  track template list                    list templates (JSON)
  track babel exec (--id N | --path P) [--name S | --ordinal N | --line N] [--yes]
                                        [--body-stdin] [--timeout D]
                                        run a source block, selected by name, ordinal, or a line
                                        inside it (JSON)
  track babel restore (--id N | --path P)
                                        list stored source block results (JSON)
  track export (--id N | --title S | --path P) [--out F] [--frontmatter] [--exports-default M]
                                        write a note out as Markdown (stdout, or JSON path with --out)
  track export-site --root N [--id N ...] --frontend <dist> --out <dir>
                                        publish selected vault notes as a static site (React frontend + JSON bundle) (JSON)
  track export-site --src <dir> [--root <name>] --frontend <dist> --out <dir>
                                        publish a directory of Markdown files as a static site (JSON)
  track render --spec <spec.json> --out <file> [--renderer echarts]
                                        render a View Spec chart, or a composed article (a spec with
                                        "blocks"), to an HTML file (JSON path);
                                        run "track render --help" for the View Spec notation
  track dump                            print placeholder state (JSON)
  track version                         print the version

Examples:
  cat article.md | track new --title "記事"
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
	// First launch: lay down the vault skeleton when the vault directory does not exist yet. An existing
	// vault is left to lazy per-kind creation, so this never resurrects directories a user removed.
	if _, statErr := os.Stat(cfg.VaultDir); os.IsNotExist(statErr) {
		if _, err := cfg.EnsureVaultSkeleton(); err != nil {
			return nil, nil, err
		}
	}
	s, err := store.Open(cfg.DBPath)
	if err != nil {
		return nil, nil, err
	}
	return cfg, s, nil
}

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
	args, ok := applyVaultFlag(args)
	if !ok {
		return 1
	}
	applyPathVault(args)
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
	case "fmt":
		return cmdFmt(rest)
	case "new":
		return cmdNew(rest)
	case "open":
		return cmdOpen(rest)
	case "journal":
		return cmdJournal(rest)
	case "append":
		return cmdAppend(rest)
	case "capture":
		return cmdCapture(rest)
	case "refile":
		return cmdRefile(rest)
	case "archive":
		return cmdArchive(rest)
	case "update":
		return cmdUpdate(rest)
	case "meta":
		return cmdMeta(rest)
	case "toggle":
		return cmdToggle(rest)
	case "task":
		return cmdTask(rest)
	case "tasks":
		return cmdTasks(rest)
	case "asset":
		return cmdAsset(rest)
	case "rename":
		return cmdRename(rest)
	case "mv":
		return cmdMv(rest)
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
	case "query":
		return cmdQuery(rest)
	case "notes":
		return cmdNotes(rest)
	case "backlinks":
		return cmdBacklinks(rest)
	case "nav":
		return cmdNav(rest)
	case "agenda":
		return cmdAgenda(rest)
	case "graph":
		return cmdGraph(rest)
	case "web":
		return cmdWeb(rest)
	case "vault":
		return cmdVault(rest)
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

Every command accepts a global --vault NAME flag selecting a named vault from the machine config's
vaults: registry (a name -> path map) for this invocation; without it the default vault is used.
An unknown name is an error, never a new vault. --vault is also the only thing that narrows search,
reindex, doctor and refresh-all back to one vault: with a registry they cover every registered vault,
and TRACK_VAULT does not scope them.

Usage:
  track vault list                      list registered vaults, marking the active one (JSON)
  track vault current                   print the active vault's name (empty when unregistered) and path (JSON)
  track vault which <name>              resolve a registered vault name to its path (JSON)
  track new --title <t> [--id <id>] [--template <s>] [--body <s>] [--tag <s>] [--parent-path P]
                                        create a note (fails if the title exists); --body is saved
                                        verbatim, and --parent-path fills a template's {{ parent }}
                                        with that note's title
  track open --title <t> [--template <s>] [--body <s>] [--tag <s>] [--parent-path P]
                                        open the note with this title, creating it if absent. --body
                                        and --tag apply only on creation: passing either when the note
                                        already exists is an error naming track append. --template is
                                        the exception and is ignored on an existing note
  track append (--id N | --title S | --path P) [--body <s>] [--tag <s>]
                                        append body text and/or merge tags into an existing note
  track capture [--target "<note>#<heading>"] [--template <s>] [--body <s>]
                                        append a (templated) entry under a heading; --target defaults
                                        to the configured capture inbox (created on first use) (JSON)
  track refile --from "<note>#<heading>" --to "<note>#<heading>" [--line N]
                                        move a heading subtree (or, with --line, one list item) to
                                        another anchor; text moves verbatim and both ends reindex (JSON)
  track archive "<note>#<heading>"      move a subtree into the archive note (per-year by default),
                                        stamping a [[link]] back to the source and the date (JSON)
  track update (--id N | --title S | --path P) [--body <s>] [--tag <s>] [--clear-tags]
                                        replace body text and/or update tags on an existing note
  track meta (--id N | --title S | --path P) [--description S] [--image assets/F] [--icon S]
             [--set key=value ...] [--unset key ...] [--edit (FILE|-)]
                                        print a note's metadata (incl. its editable YAML document
                                        under "doc"), or set it: description (og:description), cover
                                        image (og:image; an existing vault asset), the per-note icon,
                                        and typed properties (--set/--unset; comma-separated value
                                        makes a list). "up" is the conventional property that files a
                                        note under a parent, and only a [[link]] value counts.
                                        An empty description/image clears the field. --edit applies a
                                        full document (title/tags/description/image/props) from a file
                                        or stdin, validated as a whole before anything is written; a
                                        changed title renames the note, backlinks included (JSON)
  track toggle (--id N | --title S | --path P) --line N [--state toggle|check|uncheck] [--expect NAME]
                                        flip (or set) a task checkbox between the first open and the
                                        first done state; DOING and WAITING are out of its reach and
                                        need task set (JSON)
  track task set (--id N | --title S | --path P) --line N --state NAME [--expect NAME]
                                        move a task line into a named state. The set is fixed: TODO,
                                        DOING, WAITING, DONE, CANCELLED. Done-family states stamp
                                        [done:date], transitions are logged in the sidecar, and parent
                                        [n/m]/[p%] progress cookies are recomputed. --expect refuses the
                                        write unless the line is in that state (JSON)
  track task cycle (--id N | --title S | --path P) --line N [--expect NAME]
                                        advance the task one step through the state order, wrapping at
                                        the end; the same write path as task set (JSON)
  track task date (--id N | --title S | --path P) --line N [--sched YYYY-MM-DD] [--due YYYY-MM-DD]
                                        write a task's scheduled and/or due token; an empty value clears
                                        that token, and passing neither flag is an error (JSON)
  track task add (--id N | --title S | --path P) --text S [--priority A] [--sched YYYY-MM-DD]
                 [--due YYYY-MM-DD]     append a new open task line to the note's end (JSON)
  track tasks [--id N | --title S | --path P] [--state A,B] [--priority A,B] [--text S]
              [--due YYYY-MM-DD] [--overdue] [--sort priority]
                                        list indexed tasks with state/deadline/priority/text filters (JSON)
  track asset import <file>             copy a file into the vault's assets/ dir; prints the assets/<file> ref (JSON)
  track asset dir [--ensure]            print (and optionally create) the vault's assets directory (JSON)
  track rename (--id N | --title S | --path P) --to S
                                        rename a note's title and rewrite its backlinks (JSON)
  track mv (--id N | --title S | --path P) --to VAULT [--unlink | --qualify]
                                        move a note into a registered vault without leaving dead links (JSON)
  track rm (--id N | --title S | --path P)
                                        soft-delete a note: move it and its sidecar into .track/trash (JSON)
  track gen increment [--label S]       save the working vault as a new generation; drops generations
                                        past the cursor (the redo future goes even when nothing changed)
                                        and prunes old ones beyond gen_keep (JSON)
  track gen undo                        step back one generation and restore it; at a dirty head it
                                        instead auto-saves the working tree as a new generation and
                                        restores the cursor in place (JSON)
  track gen redo                        step forward one generation and restore it (JSON)
  track gen list                        list generations, the cursor, and dirty state (JSON)
  track gen status                      the file-level detail behind dirty: which files the working
                                        vault added, changed, or deleted against the cursor (JSON)
  track gen peek [--gen N] (--id N | --title S | --path P)
                                        print a note's content as of a generation (default: cursor)
  track journal [--offset <n>] [--template <s>] [--body <s>]
                                        open/create a daily note
  track init                            create the vault directory skeleton; the only command that
                                        ever creates a vault, so a typo elsewhere cannot (idempotent, JSON)
  track reindex [--full]                rebuild the index
  track doctor [--fix]                  report vault/sidecar divergence without changing files; finding
                                        issues is not a failure, so branch on .ok rather than the exit
                                        code (JSON). --fix repairs by auto-numbered restore and then
                                        reindexes: titles and tags of a missing sidecar are gone, renumbered
                                        duplicates keep no backlinks, and it demands an explicit --vault
  track refresh-all                     run the maintenance pipeline in one idempotent pass (full reindex +
                                        read-only doctor report); suitable for cron/launchd (JSON)
  track fmt [--check] (<path>... | --all)
                                         canonically format Markdown files (rewrites in place); --all
                                         covers the whole vault, --check writes nothing and exits
                                         non-zero when a file would change. Never touches fenced code (JSON)
  track keywords                        dump the auto-link dictionary (JSON)
  track resolve (--term <s> | <s>)      resolve a keyword to a note (JSON)
  track search --query <s> [--scope all|title|body] [--limit N]
                                        search notes. Space-separated terms are ANDed and an uppercase
                                        OR splits alternatives; matching is case-insensitive substring.
                                        #tag filters tags on the title path only — under --scope body a
                                        #tag term is hunted as literal body text. With a vaults: registry
                                        and no --vault this crosses every registered vault (JSON)
  track query (<expr> | --saved <name>)  run a table query over notes, e.g.
                                        'TABLE title, props.status FROM #project WHERE props.status != done
                                        SORT props.due LIMIT 10'. Bare keys are title and tags only; every
                                        user property is reached as props.<key>, and an unknown bare key
                                        is an error rather than an empty column (JSON)
  track notes [--untagged] [--limit N]  list notes, newest first; --untagged keeps only notes with no
                                        tags, for a curation pass that adds tags via track append --tag (JSON)
  track backlinks (--id N | --path P)   list backlinks (JSON)
  track nav (--id N | --path P)         print hierarchy navigation from the "up" property (up:: [[Parent]]
                                        in the body, or an up sidecar prop holding a [[link]]): the
                                        ancestor trail, root first, and the notes whose up points here.
                                        Takes no --title, and does not self-heal a stale index (JSON)
  track agenda [--date YYYY-MM-DD]       list notes created or updated on a calendar day, plus the
                                        open tasks scheduled for or due on it (JSON)
  track graph (--id N | --path P)       show a local link graph (JSON)
  track graph --orphans                 vault-wide link hygiene in one call: notes with no inbound link,
                                        and titles naming a parent scope no note owns (JSON)
  track web [--addr 127.0.0.1:8765]      serve the local web workspace
  track template new --name <s> [--id N]
                                        create a template (JSON)
  track template open --name <s>         open or create a template (JSON)
  track template list                    list templates (JSON)
  track babel exec (--id N | --path P) [--name S | --ordinal N | --line N] [--yes]
                                        [--var k=v ...] [--body-stdin] [--timeout D]
                                        run a source block, selected by name, ordinal, or a line
                                        inside it; --var feeds the block's environment and a value
                                        naming another block uses its stored result (JSON)
  track babel run --name S (--id N | --path P) [--var k=v ...]
                                        call a named block with parameters (same as exec)
  track babel tangle (--id N | --path P) [--dry-run]
                                        write blocks carrying :tangle <file> out to files inside the
                                        vault; same-target blocks concatenate in note order (JSON)
  track babel restore (--id N | --path P)
                                        list stored source block results (JSON)
  track export (--id N | --title S | --path P) [--out F] [--frontmatter] [--exports-default M]
                                        write a note out as Markdown (stdout, or JSON path with --out)
  track export-site (--all | --id N ...) [--root N] [--calendar] [--share]
                                        [--base-url URL] --frontend <dist> --out <dir>
                                        publish vault notes as a static site (React frontend + JSON
                                        bundle); --all takes every note, --root defaults to the vault
                                        config's web.home (JSON)
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
	// No vault is ever created implicitly: a missing directory is refused here rather than scaffolded,
	// because the downstream writers (createTitledNote, the sidecar writer, the journal) all MkdirAll
	// their own parents and would happily populate a typo'd or unmounted path. `track init` is the one
	// command that creates a vault.
	if err := requireVaultDir(cfg); err != nil {
		return nil, nil, err
	}
	s, err := store.Open(cfg.DBPath)
	if err != nil {
		return nil, nil, err
	}
	return cfg, s, nil
}

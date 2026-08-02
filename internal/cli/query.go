package cli

import (
	"flag"
	"strings"

	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/query"
)

// cmdQuery runs a query expression (or a saved query from config `queries:`) over the indexed notes
// and prints the result table as JSON. See the "Query" help note for the grammar.
// queryUsage documents the grammar, which no flag can show: the expression is positional.
const queryUsage = `Usage: track query '<expr>'   |   track query --saved <name>

  TABLE <cols> [FROM #tag] [WHERE <cond> AND ...] [SORT <key> [DESC]] [LIMIT n]

A column or key is either a note attribute — bare ` + "`title`" + ` or ` + "`tags`" + `, the only two — or a user
property, which is reachable only as props.<key>. Any other bare key is a loud error, never an
empty column. Conditions are #tag filters, typed comparisons (=, !=, <, >), or a bare key as a
presence check; they combine with AND only. Tags are hierarchical: #a matches #a/b, not #ab.

  track query 'TABLE title, props.status FROM #project WHERE props.status != done SORT props.due LIMIT 10'

Prints {"columns":[…],"rows":[{"note_id","title","cells"}],"count"}.
`

func cmdQuery(args []string) int {
	fs := flag.NewFlagSet("query", flag.ContinueOnError)
	saved := fs.String("saved", "", "run a saved query by name (config queries: in <vault>/.track/config.yml)")
	if code, ok := parseArgs(fs, args, queryUsage); !ok {
		return code
	}
	expr := strings.TrimSpace(strings.Join(fs.Args(), " "))

	cfg, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	if *saved != "" {
		if expr != "" {
			return fail("pass either an expression or --saved, not both")
		}
		if expr, err = query.ResolveSaved("saved: "+*saved, cfg.Queries); err != nil {
			return fail("%v", err)
		}
	}
	if expr == "" {
		return fail("a query expression (or --saved <name>) is required")
	}

	q, err := query.Parse(expr)
	if err != nil {
		return fail("parse query: %v", err)
	}

	// Self-heal before reading, like search: the index is a rebuildable cache another process may
	// have outrun, so results match the files on disk.
	if _, err := index.New(cfg, s).RefreshIfStale(); err != nil {
		return fail("refresh index: %v", err)
	}
	rows, err := query.RowsFromStore(s)
	if err != nil {
		return fail("load notes: %v", err)
	}
	res := query.Run(q, rows)
	return emit(map[string]any{"columns": res.Columns, "rows": res.Rows, "count": len(res.Rows)})
}

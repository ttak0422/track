package cli

import (
	"flag"
	"strings"

	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/task"
)

func cmdTimeline(args []string) int {
	fs := flag.NewFlagSet("timeline", flag.ContinueOnError)
	id := fs.Int64("id", 0, "note id")
	title := fs.String("title", "", "note title")
	path := fs.String("path", "", "note path")
	if code, ok := parseArgs(fs, args); !ok {
		return code
	}
	if *id == 0 && strings.TrimSpace(*title) == "" && strings.TrimSpace(*path) == "" {
		return fail("timeline requires --id, --title, or --path")
	}
	cfg, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()
	p, err := resolveNotePath(cfg, s, *id, strings.TrimSpace(*title), strings.TrimSpace(*path))
	if err != nil {
		return fail("%v", err)
	}
	n, err := note.ReadFile(p, cfg)
	if err != nil {
		return fail("read timeline: %v", err)
	}
	return emit(task.NewTimeline(n.ID, n.Meta.Title, n.Body, n.Meta.TaskLog))
}

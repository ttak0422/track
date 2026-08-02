package cli

import (
	"flag"
	"os"
	"strings"

	"github.com/ttak0422/track/internal/track/config"
	trackrename "github.com/ttak0422/track/internal/track/rename"
)

// cmdMv resolves the source and registered destination, then delegates the cross-vault mutation to
// the reusable engine path.
func cmdMv(args []string) int {
	fs := flag.NewFlagSet("mv", flag.ContinueOnError)
	id := fs.Int64("id", 0, "note id")
	title := fs.String("title", "", "note title (alternative to --id)")
	path := fs.String("path", "", "note path (alternative to --id)")
	to := fs.String("to", "", "registered destination vault name")
	unlink := fs.Bool("unlink", false, "turn destination-local links that would break into plain text")
	qualify := fs.Bool("qualify", false, "qualify destination-local links back to the source vault")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}
	dstName := strings.TrimSpace(*to)
	if dstName == "" {
		return fail("--to is required")
	}
	if *unlink && *qualify {
		return fail("--unlink and --qualify are mutually exclusive")
	}

	srcCfg, srcStore, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer srcStore.Close()
	dstRoot, ok := srcCfg.Vaults[dstName]
	if !ok {
		return fail("unknown destination vault %q", dstName)
	}
	if srcCfg.VaultName == dstName {
		return fail("destination vault %q is already active", dstName)
	}
	if st, err := os.Stat(dstRoot); err != nil || !st.IsDir() {
		return fail("destination vault %q is unavailable: %v", dstName, err)
	}

	srcPath, err := resolveNotePath(srcCfg, srcStore, *id, strings.TrimSpace(*title), strings.TrimSpace(*path))
	if err != nil {
		return fail("%v", err)
	}
	dstCfg, err := config.LoadAt(dstRoot)
	if err != nil {
		return fail("load destination vault: %v", err)
	}
	res, err := trackrename.Move(srcCfg, dstCfg, srcStore, srcPath, dstName, trackrename.MoveOptions{
		Unlink: *unlink, Qualify: *qualify,
	})
	if err != nil {
		return fail("%v", err)
	}
	return emit(map[string]any{
		"id": res.NoteID, "title": res.Title, "from": srcCfg.VaultName, "to": dstName,
		"path": res.DestinationPath, "backlinks_updated": res.BacklinksUpdated,
	})
}

package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/ttak0422/track/internal/track/note"
)

// parseArgs parses a subcommand's flags and reports whether the command should carry on. Asking for
// help is not a failure: the usage goes to stdout and the command exits 0, so an agent bound by the
// {"error":...}/exit 1 contract does not read its own help request as a failed command. A real parse
// error keeps the old shape — usage on stderr, error JSON on stdout, exit 1 — because that one is a
// failure and an agent should retry differently.
// A synopsis replaces the bare "Usage of <cmd>:" line for commands whose contract is not visible in
// their flags — an expression taken positionally, a grammar, an argument order.
func parseArgs(fs *flag.FlagSet, args []string, synopsis ...string) (int, bool) {
	var usage strings.Builder
	if len(synopsis) > 0 {
		fs.Usage = func() {
			fmt.Fprintf(fs.Output(), "%s\nFlags:\n", strings.TrimRight(synopsis[0], "\n"))
			fs.PrintDefaults()
		}
	}
	fs.SetOutput(&usage)
	err := fs.Parse(args)
	fs.SetOutput(os.Stderr)
	switch {
	case errors.Is(err, flag.ErrHelp):
		fmt.Print(usage.String())
		return 0, false
	case err != nil:
		fmt.Fprint(os.Stderr, usage.String())
		return fail("parse args: %v", err), false
	}
	return 0, true
}

// tagsFlag collects repeatable --tag values; each value may itself be comma-separated.
type tagsFlag []string

func (t *tagsFlag) String() string { return strings.Join(*t, ",") }

func (t *tagsFlag) Set(v string) error {
	for _, part := range strings.Split(v, ",") {
		if p := strings.TrimSpace(part); p != "" {
			*t = append(*t, p)
		}
	}
	return nil
}

// idsFlag collects repeatable --id values; each value may itself be comma-separated note ids.
type idsFlag []int64

func (f *idsFlag) String() string {
	parts := make([]string, len(*f))
	for i, id := range *f {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(parts, ",")
}

func (f *idsFlag) Set(v string) error {
	for _, part := range strings.Split(v, ",") {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		id, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid id %q", p)
		}
		*f = append(*f, id)
	}
	return nil
}

// kvFlag collects repeatable key=value pairs (e.g. --set status=draft). The value may contain "=".
type kvFlag []struct{ Key, Value string }

func (f *kvFlag) String() string {
	parts := make([]string, len(*f))
	for i, kv := range *f {
		parts[i] = kv.Key + "=" + kv.Value
	}
	return strings.Join(parts, " ")
}

func (f *kvFlag) Set(v string) error {
	key, value, ok := strings.Cut(v, "=")
	key = strings.TrimSpace(key)
	if !ok || key == "" {
		return fmt.Errorf("expected key=value, got %q", v)
	}
	*f = append(*f, struct{ Key, Value string }{key, strings.TrimSpace(value)})
	return nil
}

// dedupTags trims and de-duplicates tags, preserving first-seen order. It returns nil for an empty
// set. The rule itself lives in the engine (note.DedupTags) so the metadata-document editor
// normalizes tags identically.
func dedupTags(tags []string) []string {
	return note.DedupTags(tags)
}

// readBody returns body text from the --body flag when it was set, otherwise from piped stdin.
// An interactive terminal (no pipe) yields an empty body instead of blocking on a read.
func readBody(fs *flag.FlagSet, flagVal string) (string, error) {
	if flagWasSet(fs, "body") {
		return flagVal, nil
	}
	fi, err := os.Stdin.Stat()
	if err != nil {
		return "", nil
	}
	if fi.Mode()&os.ModeCharDevice != 0 {
		return "", nil
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// flagWasSet reports whether the named flag was explicitly provided on the command line.
func flagWasSet(fs *flag.FlagSet, name string) bool {
	found := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

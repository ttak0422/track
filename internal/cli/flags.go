package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

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

// dedupTags trims and de-duplicates tags, preserving first-seen order. It returns nil for an empty set.
func dedupTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(tags))
	var out []string
	for _, tg := range tags {
		tg = strings.TrimSpace(tg)
		if tg == "" || seen[tg] {
			continue
		}
		seen[tg] = true
		out = append(out, tg)
	}
	return out
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

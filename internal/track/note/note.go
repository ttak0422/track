// Package note models a track note: a markdown body plus a footmatter metadata
// block. It owns parsing notes off disk and the footmatter split/serialize
// logic shared by the indexer and CLI.
package note

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ttak0422/track/internal/track/config"
)

// Footmatter is the structured metadata stored at the end of a note. Created is
// kept as a string so YAML round-trips it verbatim instead of reformatting a
// time.Time.
type Footmatter struct {
	Title   string   `yaml:"title,omitempty"`
	Aliases []string `yaml:"aliases,omitempty"`
	Tags    []string `yaml:"tags,omitempty"`
	Created string   `yaml:"created,omitempty"`
}

type Note struct {
	ID    int64
	Path  string
	Body  string
	Mtime int64
	Foot  Footmatter
}

// ParseFile reads a note from disk, splitting body and footmatter and deriving
// the id from the filename.
func ParseFile(path string, c *config.Config) (*Note, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	body, f, _, err := SplitFootmatter(string(raw), c.Footmatter)
	if err != nil {
		return nil, err
	}
	id, err := IDFromPath(path)
	if err != nil {
		return nil, err
	}
	return &Note{ID: id, Path: path, Body: body, Mtime: info.ModTime().Unix(), Foot: f}, nil
}

// IDFromPath extracts the unix-timestamp id encoded in a note's filename.
func IDFromPath(path string) (int64, error) {
	base := filepath.Base(path)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	return strconv.ParseInt(name, 10, 64)
}

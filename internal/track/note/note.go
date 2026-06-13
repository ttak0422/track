// Package note models a track note: a markdown body plus versioned sidecar metadata stored under the vault's .track directory.
// It owns parsing notes off disk and the metadata read/write logic shared by the indexer and CLI.
package note

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/track/babel"
	"github.com/ttak0422/track/internal/track/config"
)

const (
	// GeneratedByAITag marks provenance, not quality: generated notes stay searchable,
	// but search and graph UIs may use this tag as a light ranking/display signal.
	GeneratedByAITag = "generated-by-ai"
)

// Metadata is the structured data stored beside a note under .track/notes.
// Created is kept as a string so YAML round-trips it verbatim instead of reformatting a time.Time.
// Blocks holds Babel source-block results, keyed by block id; it is only present in version 2 sidecars.
type Metadata struct {
	Version int                        `yaml:"version"`
	Title   string                     `yaml:"title,omitempty"`
	Tags    []string                   `yaml:"tags,omitempty"`
	Created string                     `yaml:"created,omitempty"`
	Blocks  map[string]babel.BlockMeta `yaml:"blocks,omitempty"`
}

type Note struct {
	ID    int64
	Kind  string
	Path  string
	Body  string
	Mtime int64
	Meta  Metadata
}

// ParseFile reads a note from disk, deriving the id from the filename and loading its sidecar metadata.
// For compatibility with early track notes, a legacy trailing footmatter block is used only when no sidecar exists.
//
// The sidecar metadata is authoritative for the title and other structured fields: the body is plain
// content. A note's title comes from the sidecar and changes only through the create/rename commands,
// never by editing a body heading, so the body may contain any headings (including a leading H1).
func ParseFile(path string, c *config.Config) (*Note, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	id, err := IDFromPath(path)
	if err != nil {
		return nil, err
	}
	kind, ok := c.KindFromPath(path)
	if !ok {
		kind = config.KindNote
	}
	body, legacy, hasLegacy := SplitLegacyFootmatter(string(raw))

	metaPath := c.MetadataPath(id)
	meta, found, err := ReadMetadata(metaPath)
	if err != nil {
		return nil, err
	}
	metadataSource := found
	if !found && hasLegacy {
		meta, err = ParseLegacyMetadata(legacy)
		if err != nil {
			return nil, err
		}
		metadataSource = true
	}
	if meta.Version == 0 {
		meta.Version = CurrentMetadataVersion
	}

	// A freshly migrated legacy footmatter note has no sidecar yet; persist one so later reads are
	// metadata-driven. Sidecars that already exist are never rewritten from the body.
	if !found && metadataSource {
		if err := WriteMetadata(metaPath, meta); err != nil {
			return nil, err
		}
	}
	return &Note{ID: id, Kind: kind, Path: path, Body: body, Mtime: info.ModTime().Unix(), Meta: meta}, nil
}

// IDFromPath extracts the numeric id encoded in a note's filename.
func IDFromPath(path string) (int64, error) {
	base := filepath.Base(path)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	return IDFromName(name)
}

// IDFromName extracts a numeric note id from a basename without extension.
func IDFromName(name string) (int64, error) {
	return strconv.ParseInt(name, 10, 64)
}

// NewID returns a sortable note id for t.
// The id is Unix seconds shifted three decimal places, plus the first free same-second sequence
// number. For example, 2026-06-04T12:00:00Z starts at ...000; subsequent notes in that same second
// take ...001, ...002, and so on.
func NewID(c *config.Config, t time.Time) (int64, error) {
	return FreeID(c, t.Unix()*1000)
}

// FreeID returns the first note id at or after start whose note file does not yet exist.
// Callers usually pass a second-based bucket from NewID; scanning upward guarantees that notes
// created in the same second never collide or overwrite each other.
func FreeID(c *config.Config, start int64) (int64, error) {
	for id := start; ; id++ {
		_, err := os.Stat(c.NotePath(id))
		if os.IsNotExist(err) {
			return id, nil
		}
		if err != nil {
			return 0, err
		}
	}
}

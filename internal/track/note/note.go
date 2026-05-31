// Package note models a track note: a markdown body plus versioned sidecar metadata stored under the vault's .track directory.
// It owns parsing notes off disk and the metadata read/write logic shared by the indexer and CLI.
package note

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ttak0422/track/internal/track/babel"
	"github.com/ttak0422/track/internal/track/config"
)

// Metadata is the structured data stored beside a note under .track/notes.
// Created is kept as a string so YAML round-trips it verbatim instead of reformatting a time.Time.
// Blocks holds Babel source-block results, keyed by block id; it is only present in version 2 sidecars.
type Metadata struct {
	Version int                        `yaml:"version"`
	Title   string                     `yaml:"title,omitempty"`
	Aliases []string                   `yaml:"aliases,omitempty"`
	Tags    []string                   `yaml:"tags,omitempty"`
	Created string                     `yaml:"created,omitempty"`
	Blocks  map[string]babel.BlockMeta `yaml:"blocks,omitempty"`
}

type Note struct {
	ID    int64
	Path  string
	Body  string
	Mtime int64
	Meta  Metadata
}

// ParseFile reads a note from disk, deriving the id from the filename and loading its sidecar metadata.
// For compatibility with early track notes, a legacy trailing footmatter block is used only when no sidecar exists.
//
// The note body is authoritative for fields it can express.
// Today that means the first H1 heading owns the note title; if sidecar metadata disagrees, the sidecar is rewritten to match the body while preserving aliases, tags, and created.
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

	dirty := !found && metadataSource
	if title := FirstH1Title(body); title != "" && meta.Title != title {
		meta.Title = title
		dirty = true
	}
	if dirty {
		if err := WriteMetadata(metaPath, meta); err != nil {
			return nil, err
		}
	}
	return &Note{ID: id, Path: path, Body: body, Mtime: info.ModTime().Unix(), Meta: meta}, nil
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

// FreeID returns the first note id at or after start whose note file does not yet exist.
// Callers derive start from a timestamp (e.g. time.Now().UnixMilli()); scanning upward guarantees
// that notes created in the same instant—such as a batch of machine-generated notes—never collide
// or overwrite each other, the later ones simply taking the next free id.
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

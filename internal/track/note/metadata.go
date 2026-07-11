package note

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// CurrentMetadataVersion is stamped on new sidecars that carry no Babel results.
	CurrentMetadataVersion = 1
	// MetadataVersionV2 adds Babel source-block results under blocks.
	MetadataVersionV2 = 2
	// MetadataVersionV3 adds the activity day list under days.
	MetadataVersionV3 = 3
	// MetadataVersionV4 adds the page metadata fields description and image.
	MetadataVersionV4 = 4
	// MetadataVersionV5 adds the per-note icon override.
	MetadataVersionV5 = 5
	// MaxMetadataVersion is the newest schema this build can read and write.
	MaxMetadataVersion = MetadataVersionV5
)

func supportedVersion(v int) bool {
	return v >= CurrentMetadataVersion && v <= MaxMetadataVersion
}

// ReadMetadata reads a versioned metadata sidecar. found=false means the note has no sidecar yet.
func ReadMetadata(path string) (meta Metadata, found bool, err error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Metadata{}, false, nil
		}
		return Metadata{}, false, err
	}
	if err := yaml.Unmarshal(raw, &meta); err != nil {
		return Metadata{}, true, err
	}
	if !supportedVersion(meta.Version) {
		return Metadata{}, true, fmt.Errorf("unsupported metadata version %d in %s", meta.Version, path)
	}
	return meta, true, nil
}

// WriteMetadata writes a note's versioned sidecar metadata, creating the containing .track/notes directory when needed.
// Sidecars stay at version 1 until they carry Babel block results (version 2) or activity days (version 3).
func WriteMetadata(path string, meta Metadata) error {
	if meta.Version == 0 {
		meta.Version = CurrentMetadataVersion
	}
	if len(meta.Blocks) > 0 && meta.Version < MetadataVersionV2 {
		meta.Version = MetadataVersionV2
	}
	if len(meta.Days) > 0 && meta.Version < MetadataVersionV3 {
		meta.Version = MetadataVersionV3
	}
	if (meta.Description != "" || meta.Image != "") && meta.Version < MetadataVersionV4 {
		meta.Version = MetadataVersionV4
	}
	if meta.Icon != "" && meta.Version < MetadataVersionV5 {
		meta.Version = MetadataVersionV5
	}
	if !supportedVersion(meta.Version) {
		return fmt.Errorf("unsupported metadata version %d", meta.Version)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	out, err := yaml.Marshal(meta)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}

// ActivityDays returns the days a note surfaces on in day-indexed views (the agenda, the calendar, the
// activity heatmap). Journals contribute none — a day's journal (and the month/year summaries) would
// otherwise count as activity on every day it is opened. A sidecar predating the Days field falls back
// to the created day, so the note still surfaces on the day it was made. This is the single rule the
// index (note_days) and the static-site export both apply, so live and published calendars agree.
func ActivityDays(kind string, meta Metadata) []string {
	if kind == "journal" {
		return nil
	}
	if len(meta.Days) == 0 {
		if meta.Created == "" {
			return nil
		}
		return []string{meta.Created}
	}
	return meta.Days
}

// EnsureDay returns meta with day recorded in its sorted, deduplicated Days set, and reports whether
// that changed anything. An empty day is ignored. Callers persist the result with WriteMetadata only
// when changed is true, so re-indexing an unchanged note never rewrites its sidecar.
func EnsureDay(meta Metadata, day string) (Metadata, bool) {
	if day == "" {
		return meta, false
	}
	i, found := slices.BinarySearch(meta.Days, day)
	if found {
		return meta, false
	}
	meta.Days = slices.Insert(meta.Days, i, day)
	return meta, true
}

// SplitLegacyFootmatter strips the old trailing HTML-comment metadata block from a note body.
// The returned yamlText is for migration/compatibility only; new writes use sidecar metadata files instead.
func SplitLegacyFootmatter(raw string) (body string, yamlText string, found bool) {
	const open = "<!--track"
	const close = "-->"

	lines := strings.Split(raw, "\n")
	openIdx, closeIdx := -1, -1
	for i := len(lines) - 1; i >= 0; i-- {
		t := strings.TrimSpace(lines[i])
		if closeIdx == -1 {
			if t == close {
				closeIdx = i
			}
			continue
		}
		if t == open {
			openIdx = i
			break
		}
	}
	if openIdx == -1 || closeIdx == -1 || closeIdx <= openIdx {
		return strings.TrimRight(raw, "\n"), "", false
	}

	body = strings.TrimRight(strings.Join(lines[:openIdx], "\n"), "\n")
	yamlText = strings.Join(lines[openIdx+1:closeIdx], "\n")
	return body, yamlText, true
}

// ParseLegacyMetadata parses the old footmatter payload into the current metadata shape.
func ParseLegacyMetadata(yamlText string) (Metadata, error) {
	var meta Metadata
	if err := yaml.Unmarshal([]byte(yamlText), &meta); err != nil {
		return Metadata{}, err
	}
	if meta.Version == 0 {
		meta.Version = CurrentMetadataVersion
	}
	return meta, nil
}

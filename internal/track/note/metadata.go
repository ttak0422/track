package note

import (
	"errors"
	"fmt"
	"github.com/ttak0422/track/internal/track/notedata"
	"github.com/ttak0422/track/internal/track/vaultfile"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"time"

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
	// MetadataVersionV5 adds typed note properties under props.
	MetadataVersionV5 = 5
	// MetadataVersionV6 adds the per-note icon override.
	MetadataVersionV6 = 6
	// MetadataVersionV7 adds the task state transition log under task_log.
	MetadataVersionV7 = 7
	// MetadataVersionV8 adds the pinned published slug under slug.
	MetadataVersionV8 = 8
	// MetadataVersionV9 adds the shared reading milestones seen_at and read_at.
	MetadataVersionV9 = 9
	// MetadataVersionV10 adds the author-assigned flags (the implementation-defined
	// closed set DEPRECATED/CONFIDENTIAL, ADR 0074) under flags.
	MetadataVersionV10 = 10
	// MetadataVersionV11 adds the append-only agent execution log under exec_log.
	MetadataVersionV11 = 11
	// MetadataVersionV12 adds Babel input hashes and execution cache keys.
	MetadataVersionV12 = 12
	// MaxMetadataVersion is the newest schema this build can read and write.
	MaxMetadataVersion = notedata.MaxMetadataVersion
)

func supportedVersion(v int) bool {
	return v >= CurrentMetadataVersion && v <= MaxMetadataVersion
}

// ReadMetadata reads a versioned metadata sidecar. found=false means the note has no sidecar yet.
func ReadMetadata(path string) (meta Metadata, found bool, err error) {
	raw, err := vaultfile.Read(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if _, linkErr := vaultfile.Lstat(path); linkErr == nil {
				return Metadata{}, false, vaultfile.ReadError(path, err)
			} else if !errors.Is(linkErr, os.ErrNotExist) {
				return Metadata{}, false, vaultfile.ReadError(path, linkErr)
			}
			return Metadata{}, false, nil
		}
		return Metadata{}, false, err
	}
	if err := yaml.Unmarshal(raw, &meta); err != nil {
		return Metadata{}, true, &ParseError{Path: path, Err: err}
	}
	if !supportedVersion(meta.Version) {
		return Metadata{}, true, &ParseError{Path: path, Err: fmt.Errorf("unsupported metadata version %d", meta.Version)}
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
	if len(meta.Props) > 0 && meta.Version < MetadataVersionV5 {
		meta.Version = MetadataVersionV5
	}
	if meta.Icon != "" && meta.Version < MetadataVersionV6 {
		meta.Version = MetadataVersionV6
	}
	if len(meta.TaskLog) > 0 && meta.Version < MetadataVersionV7 {
		meta.Version = MetadataVersionV7
	}
	if meta.Slug != "" && meta.Version < MetadataVersionV8 {
		meta.Version = MetadataVersionV8
	}
	if (meta.SeenAt != "" || meta.ReadAt != "") && meta.Version < MetadataVersionV9 {
		meta.Version = MetadataVersionV9
	}
	if len(meta.Flags) > 0 && meta.Version < MetadataVersionV10 {
		meta.Version = MetadataVersionV10
	}
	if len(meta.ExecLog) > 0 && meta.Version < MetadataVersionV11 {
		meta.Version = MetadataVersionV11
	}
	for _, block := range meta.Blocks {
		if (block.InputHash != "" || block.ExecutionKey != "") && meta.Version < MetadataVersionV12 {
			meta.Version = MetadataVersionV12
			break
		}
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

// StampUnix parses a sidecar timestamp into unix seconds for the index and API payloads: 0 when
// absent, and 1 for a present-but-unparseable value so a hand-edited stamp still reads as "happened"
// rather than silently dropping the milestone.
func StampUnix(stamp string) int64 {
	if stamp == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return 1
	}
	return t.Unix()
}

// SplitLegacyFootmatter strips the old trailing HTML-comment metadata block from a note body.
// The returned yamlText is for migration/compatibility only; new writes use sidecar metadata files instead.
func SplitLegacyFootmatter(raw string) (string, string, bool) {
	return notedata.SplitLegacyFootmatter(raw)
}

func ParseLegacyMetadata(raw string) (Metadata, error) { return notedata.ParseLegacyMetadata(raw) }

// WriteMetadataAtomic protects the existing sidecar during optional background
// activity updates, including process shutdown before the replacement is ready.
func WriteMetadataAtomic(path string, expected, meta Metadata) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".activity-*.yaml.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Close(); err != nil {
		return err
	}
	if err := WriteMetadata(tmp, meta); err != nil {
		return err
	}
	info, err := vaultfile.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("activity metadata must be a regular sidecar")
	}
	if err := os.Chmod(tmp, info.Mode().Perm()); err != nil {
		return err
	}
	current, found, err := ReadMetadata(path)
	if err != nil {
		return err
	}
	if !found || !reflect.DeepEqual(current, expected) {
		return errors.New("metadata changed before activity update")
	}
	return os.Rename(tmp, path)
}

package note

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// CurrentMetadataVersion is stamped on new sidecars that carry no Babel results.
	CurrentMetadataVersion = 1
	// MetadataVersionV2 adds Babel source-block results under blocks.
	MetadataVersionV2 = 2
	// MaxMetadataVersion is the newest schema this build can read and write.
	MaxMetadataVersion = MetadataVersionV2
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
// Sidecars stay at version 1 until they carry Babel block results, which require version 2.
func WriteMetadata(path string, meta Metadata) error {
	if meta.Version == 0 {
		meta.Version = CurrentMetadataVersion
	}
	if len(meta.Blocks) > 0 && meta.Version < MetadataVersionV2 {
		meta.Version = MetadataVersionV2
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

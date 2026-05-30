package note

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const CurrentMetadataVersion = 1

// ReadMetadata reads a versioned metadata sidecar. found=false means the note
// has no sidecar yet.
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
	if meta.Version != CurrentMetadataVersion {
		return Metadata{}, true, fmt.Errorf("unsupported metadata version %d in %s", meta.Version, path)
	}
	return meta, true, nil
}

// WriteMetadata writes a note's versioned sidecar metadata, creating the
// containing .track/notes directory when needed.
func WriteMetadata(path string, meta Metadata) error {
	if meta.Version == 0 {
		meta.Version = CurrentMetadataVersion
	}
	if meta.Version != CurrentMetadataVersion {
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

// SplitLegacyFootmatter strips the old trailing HTML-comment metadata block
// from a note body. The returned yamlText is for migration/compatibility only;
// new writes use sidecar metadata files instead.
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

// ParseLegacyMetadata parses the old footmatter payload into the current
// metadata shape.
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

// FirstH1Title returns the first markdown H1 title from body. Fenced code
// blocks are ignored so examples do not become metadata.
func FirstH1Title(body string) string {
	inFence := false
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			continue
		}
		if inFence || !strings.HasPrefix(trimmed, "# ") {
			continue
		}
		title := strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
		return strings.TrimSpace(strings.TrimRight(title, "#"))
	}
	return ""
}

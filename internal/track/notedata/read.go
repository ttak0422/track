// Package notedata is the side-effect-free note schema shared with read workers.
package notedata

import (
	"fmt"
	"github.com/ttak0422/track/internal/track/babel"
	"github.com/ttak0422/track/internal/track/dispatch"
	"github.com/ttak0422/track/internal/track/task"
	"gopkg.in/yaml.v3"
	"strings"
	"time"
)

const MaxMetadataVersion = 12

type Metadata struct {
	Version     int                        `yaml:"version"`
	Title       string                     `yaml:"title,omitempty"`
	Tags        []string                   `yaml:"tags,omitempty"`
	Created     string                     `yaml:"created,omitempty"`
	Days        []string                   `yaml:"days,omitempty"`
	Description string                     `yaml:"description,omitempty"`
	Image       string                     `yaml:"image,omitempty"`
	Props       map[string]any             `yaml:"props,omitempty"`
	Icon        string                     `yaml:"icon,omitempty"`
	SeenAt      string                     `yaml:"seen_at,omitempty"`
	ReadAt      string                     `yaml:"read_at,omitempty"`
	Flags       []string                   `yaml:"flags,omitempty"`
	Blocks      map[string]babel.BlockMeta `yaml:"blocks,omitempty"`
	TaskLog     []task.LogEntry            `yaml:"task_log,omitempty"`
	ExecLog     []dispatch.Record          `yaml:"exec_log,omitempty"`
	// Slug pins this note's published URL. The static export normally derives a slug from the note
	// id, so a note that already has a public URL under some other id — one imported from a
	// published directory — would move. Setting it here freezes the address the note is already
	// reachable at; empty means derive it as usual.
	Slug string `yaml:"slug,omitempty"`
	// Preserve extension fields through edits and reindexes. They are not editable
	// through the metadata form or part of the public JSON representation.
	Extra map[string]any `yaml:",inline" json:"-"`
}

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

func ParseLegacyMetadata(yamlText string) (Metadata, error) {
	var meta Metadata
	if err := yaml.Unmarshal([]byte(yamlText), &meta); err != nil {
		return Metadata{}, err
	}
	if meta.Version == 0 {
		meta.Version = 1
	}
	if meta.Version < 1 || meta.Version > MaxMetadataVersion {
		return Metadata{}, fmt.Errorf("unsupported metadata version %d", meta.Version)
	}
	return meta, nil
}

func (m *Metadata) ApplyReadEvent(event string, at time.Time) bool {
	stamp := at.Format(time.RFC3339)
	switch event {
	case "seen":
		if m.SeenAt != "" {
			return false
		}
		m.SeenAt = stamp
	case "read":
		if m.ReadAt != "" {
			return false
		}
		m.ReadAt = stamp
		if m.SeenAt == "" {
			m.SeenAt = stamp
		}
	default:
		return false
	}
	return true
}

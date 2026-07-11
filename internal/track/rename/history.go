// Package rename owns note title renames: the rename operation itself (uniqueness check, backlink
// rewrite, sidecar write, reindex) and the rename history used for repair suggestions.
package rename

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Entry records a title change. It is not a link keyword; it only feeds repair suggestions.
type Entry struct {
	From   string `yaml:"from"`
	To     string `yaml:"to"`
	NoteID int64  `yaml:"note_id"`
	At     string `yaml:"at"`
}

type historyFile struct {
	Version int     `yaml:"version"`
	Renames []Entry `yaml:"renames,omitempty"`
}

// Append records one title rename in path, creating the history file when needed.
func Append(path string, entry Entry) error {
	if entry.From == "" || entry.To == "" || entry.From == entry.To {
		return nil
	}
	h, err := read(path)
	if err != nil {
		return err
	}
	if entry.At == "" {
		entry.At = time.Now().UTC().Format(time.RFC3339)
	}
	h.Renames = append(h.Renames, entry)
	return write(path, h)
}

// LatestTo returns the newest destination for from, if recorded.
func LatestTo(path, from string) (Entry, bool, error) {
	h, err := read(path)
	if err != nil {
		return Entry{}, false, err
	}
	entry, ok := latestTo(h.Renames, from)
	return entry, ok, nil
}

// LatestReachable follows rename chains and returns the newest destination that currently exists.
func LatestReachable(path, from string, exists func(string) bool) (Entry, bool, error) {
	h, err := read(path)
	if err != nil {
		return Entry{}, false, err
	}
	seen := map[string]bool{from: true}
	current := from
	var last Entry
	for {
		entry, ok := latestTo(h.Renames, current)
		if !ok {
			break
		}
		last = entry
		if exists(entry.To) {
			return entry, true, nil
		}
		if seen[entry.To] {
			break
		}
		seen[entry.To] = true
		current = entry.To
	}
	if last.To != "" && exists(last.To) {
		return last, true, nil
	}
	return Entry{}, false, nil
}

func latestTo(entries []Entry, from string) (Entry, bool) {
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].From == from {
			return entries[i], true
		}
	}
	return Entry{}, false
}

func read(path string) (historyFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return historyFile{Version: 1}, nil
		}
		return historyFile{}, err
	}
	var h historyFile
	if err := yaml.Unmarshal(raw, &h); err != nil {
		return historyFile{}, err
	}
	if h.Version == 0 {
		h.Version = 1
	}
	return h, nil
}

func write(path string, h historyFile) error {
	if h.Version == 0 {
		h.Version = 1
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	out, err := yaml.Marshal(h)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}

// Package provenance preserves evidence independently of mutable notes and vault generations.
package provenance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/note"
)

type Reference struct {
	NoteID  int64  `json:"note_id"`
	Version string `json:"version"`
}

type Record struct {
	Schema int `json:"schema"`
	Reference
	Kind         string      `json:"kind"`
	Title        string      `json:"title"`
	Body         string      `json:"body"`
	ContentHash  string      `json:"content_hash"`
	Source       string      `json:"source,omitempty"`
	Format       string      `json:"format"`
	RecordedAt   string      `json:"recorded_at"`
	Inputs       []Reference `json:"inputs,omitempty"`
	Method       string      `json:"method,omitempty"`
	Settings     string      `json:"settings,omitempty"`
	Run          string      `json:"run,omitempty"`
	OriginalName string      `json:"original_name,omitempty"`
	OriginalHash string      `json:"original_hash,omitempty"`
}

type Options struct {
	Source   string
	Format   string
	At       string
	Inputs   []Reference
	Method   string
	Settings string
	Run      string
	Original string // optional local file; saved bytes never follow this path again
}

func digest(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }

func validVersion(version string) bool {
	b, err := hex.DecodeString(version)
	return err == nil && len(b) == sha256.Size && strings.ToLower(version) == version
}

func recordDir(cfg *config.Config, ref Reference) (string, error) {
	if ref.NoteID <= 0 || !validVersion(ref.Version) {
		return "", fmt.Errorf("a positive note id and a lowercase SHA-256 version are required")
	}
	return filepath.Join(cfg.TrackDir(), "sources", strconv.FormatInt(ref.NoteID, 10), ref.Version), nil
}

// identity excludes observation time/title. Derived records use the declared recipe rather than
// output text: retrying a nondeterministic computation reuses the first result unless Run changes.
func identity(r Record) string {
	key := struct {
		Kind         string
		Source       string
		Format       string
		ContentHash  string
		OriginalHash string
		Inputs       []Reference
		Method       string
		Settings     string
		Run          string
	}{Kind: r.Kind, Source: r.Source, Format: r.Format, Inputs: r.Inputs, Method: r.Method, Settings: r.Settings, Run: r.Run}
	if r.Kind == "source" {
		key.ContentHash, key.OriginalHash = r.ContentHash, r.OriginalHash
	}
	raw, _ := json.Marshal(key)
	return digest(raw)
}

// Current reads a note by stable vault-local ID without relying on a rebuildable index.
func Current(cfg *config.Config, id int64) (*note.Note, error) {
	if id <= 0 {
		return nil, fmt.Errorf("a positive note id is required")
	}
	for _, kind := range []string{config.KindNote, config.KindJournal} {
		n, err := note.ParseFile(cfg.PathForKind(kind, id), cfg)
		if err == nil {
			// Evidence keeps exact bytes; the normal note parser trims trailing whitespace.
			raw, err := os.ReadFile(n.Path)
			if err != nil {
				return nil, err
			}
			n.Body = string(raw)
			return n, nil
		}
		if !os.IsNotExist(err) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("note %d does not exist", id)
}

// Read validates the saved bytes and requires the owning note to still exist. It never substitutes
// the working body or a newer version when evidence is missing or corrupt.
func Read(cfg *config.Config, ref Reference) (Record, error) {
	dir, err := recordDir(cfg, ref)
	if err != nil {
		return Record{}, err
	}
	if _, err := Current(cfg, ref.NoteID); err != nil {
		return Record{}, err
	}
	raw, err := os.ReadFile(filepath.Join(dir, "record.json"))
	if err != nil {
		return Record{}, fmt.Errorf("read source version: %w", err)
	}
	var r Record
	if err := json.Unmarshal(raw, &r); err != nil {
		return Record{}, fmt.Errorf("decode source version: %w", err)
	}
	if r.Schema != 1 || r.Reference != ref || (r.Kind != "source" && r.Kind != "derived") || digest([]byte(r.Body)) != r.ContentHash || identity(r) != r.Version {
		return Record{}, fmt.Errorf("source version %s has invalid metadata or content hash", ref.Version)
	}
	if _, err := time.Parse(time.RFC3339Nano, r.RecordedAt); err != nil {
		return Record{}, fmt.Errorf("invalid recorded_at: %w", err)
	}
	if r.OriginalHash != "" {
		raw, err := os.ReadFile(filepath.Join(dir, "original"))
		if err != nil {
			return Record{}, fmt.Errorf("read original: %w", err)
		}
		if digest(raw) != r.OriginalHash {
			return Record{}, fmt.Errorf("original content hash mismatch")
		}
	}
	return r, nil
}

// Save freezes an existing note. The note remains the editable/searchable view; the snapshot is
// authoritative evidence. A fully staged directory is published with one rename, so interrupted
// writes cannot become successful records. Parallel identical saves converge on the same version.
func Save(cfg *config.Config, id int64, opts Options) (Record, bool, error) {
	n, err := Current(cfg, id)
	if err != nil {
		return Record{}, false, err
	}
	if !utf8.ValidString(n.Body) {
		return Record{}, false, fmt.Errorf("source body must be UTF-8")
	}
	if strings.TrimSpace(opts.Format) == "" {
		return Record{}, false, fmt.Errorf("format is required")
	}
	at, err := time.Parse(time.RFC3339Nano, opts.At)
	if err != nil {
		return Record{}, false, fmt.Errorf("at must include a timezone (RFC3339): %w", err)
	}
	r := Record{Schema: 1, Reference: Reference{NoteID: id}, Kind: "source", Title: n.Meta.Title, Body: n.Body, ContentHash: digest([]byte(n.Body)), Source: opts.Source, Format: opts.Format, RecordedAt: at.UTC().Format(time.RFC3339Nano)}
	var original []byte
	if len(opts.Inputs) > 0 {
		if opts.Source != "" || opts.Original != "" || strings.TrimSpace(opts.Method) == "" || strings.TrimSpace(opts.Settings) == "" {
			return Record{}, false, fmt.Errorf("derived records require method and settings, and forbid source/original")
		}
		r.Kind, r.Method, r.Settings, r.Run = "derived", opts.Method, opts.Settings, opts.Run
		r.Inputs = slices.Clone(opts.Inputs)
		slices.SortFunc(r.Inputs, func(a, b Reference) int {
			if a.NoteID < b.NoteID {
				return -1
			}
			if a.NoteID > b.NoteID {
				return 1
			}
			return strings.Compare(a.Version, b.Version)
		})
		r.Inputs = slices.Compact(r.Inputs)
		for _, ref := range r.Inputs {
			if _, err := Read(cfg, ref); err != nil {
				return Record{}, false, fmt.Errorf("input %d:%s: %w", ref.NoteID, ref.Version, err)
			}
		}
	} else {
		if strings.TrimSpace(opts.Source) == "" || opts.Method != "" || opts.Settings != "" || opts.Run != "" {
			return Record{}, false, fmt.Errorf("source records require source and forbid method/settings/run without inputs")
		}
		if opts.Original != "" {
			original, err = os.ReadFile(opts.Original)
			if err != nil {
				return Record{}, false, fmt.Errorf("read original: %w", err)
			}
			r.OriginalName, r.OriginalHash = filepath.Base(opts.Original), digest(original)
		}
	}
	r.Version = identity(r)
	root := filepath.Join(cfg.TrackDir(), "sources")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return Record{}, false, err
	}
	lock, err := os.OpenFile(filepath.Join(root, ".save.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return Record{}, false, err
	}
	defer lock.Close() // Closing also releases the process lock after failures or a crash.
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return Record{}, false, fmt.Errorf("lock source saves: %w", err)
	}
	// ponytail: one vault lock and O(n) owner scan; index identities if source volume demands it.
	entries, err := os.ReadDir(root)
	if err != nil {
		return Record{}, false, err
	}
	var existing Record
	for _, entry := range entries {
		owner, err := strconv.ParseInt(entry.Name(), 10, 64)
		if !entry.IsDir() || err != nil || owner <= 0 || strconv.FormatInt(owner, 10) != entry.Name() {
			continue
		}
		ref := Reference{NoteID: owner, Version: r.Version}
		dir, _ := recordDir(cfg, ref)
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return Record{}, false, err
		}
		candidate, err := Read(cfg, ref)
		if err != nil {
			return Record{}, false, fmt.Errorf("existing source %d:%s: %w", owner, r.Version, err)
		}
		// Validate every matching legacy copy; do not hide a broken owner behind a healthy one.
		if existing.NoteID == 0 || owner < existing.NoteID {
			existing = candidate
		}
	}
	if existing.NoteID != 0 {
		return existing, false, nil
	}
	dir, _ := recordDir(cfg, r.Reference)
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return Record{}, false, err
	}
	stage, err := os.MkdirTemp(parent, ".pending-")
	if err != nil {
		return Record{}, false, err
	}
	defer os.RemoveAll(stage)
	if r.OriginalHash != "" {
		if err := note.WriteVerify(filepath.Join(stage, "original"), original); err != nil {
			return Record{}, false, err
		}
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return Record{}, false, err
	}
	if err := note.WriteVerify(filepath.Join(stage, "record.json"), raw); err != nil {
		return Record{}, false, err
	}
	if err := os.Rename(stage, dir); err != nil {
		return Record{}, false, fmt.Errorf("publish source version: %w", err)
	}
	return r, true, nil
}

func List(cfg *config.Config, id int64) ([]Record, error) {
	if _, err := Current(cfg, id); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(cfg.TrackDir(), "sources", strconv.FormatInt(id, 10)))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	records := []Record{}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".pending-") {
			continue
		}
		if !entry.IsDir() || !validVersion(entry.Name()) {
			return nil, fmt.Errorf("invalid source version entry %q", entry.Name())
		}
		r, err := Read(cfg, Reference{NoteID: id, Version: entry.Name()})
		if err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	slices.SortFunc(records, func(a, b Record) int {
		at, _ := time.Parse(time.RFC3339Nano, a.RecordedAt)
		bt, _ := time.Parse(time.RFC3339Nano, b.RecordedAt)
		if c := at.Compare(bt); c != 0 {
			return c
		}
		return strings.Compare(a.Version, b.Version)
	})
	return records, nil
}

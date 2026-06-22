// Package template owns track's note/journal templates: resolving a template spec (a user template by
// name/path or a shipped builtin), rendering its body, listing and creating templates, and choosing the
// default template for a kind. It is engine code with no dependency on the CLI command layer, so the CLI,
// the indexer, and the web server can all reuse it.
package template

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ttak0422/track/builtin"
	"github.com/ttak0422/track/internal/track/config"
)

// Ref identifies a template file (or a shipped builtin) without its body.
type Ref struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Path        string `json:"path"`
	ContentHash string `json:"content_hash"`
	Builtin     bool   `json:"builtin,omitempty"`
}

type data struct {
	ref  Ref
	body string
}

// Create writes a new template file for name and returns its ref. It fails if a template already resolves
// for name (or, unless overwrite, if the target file exists). When id is 0 a free id is allocated.
func Create(cfg *config.Config, name string, id int64, overwrite bool) (Ref, error) {
	if name == "" {
		return Ref{}, fmt.Errorf("--name is required")
	}
	if _, found, err := FindByName(cfg, name); err != nil {
		return Ref{}, err
	} else if found {
		return Ref{}, fmt.Errorf("template already exists for name %q", name)
	}
	var err error
	if id == 0 {
		id, err = freeID(cfg, time.Now())
		if err != nil {
			return Ref{}, err
		}
	}
	path := cfg.TemplatePath(id)
	if !overwrite {
		if _, err := os.Stat(path); err == nil {
			return Ref{}, fmt.Errorf("template already exists: %s", path)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Ref{}, fmt.Errorf("create template dir: %v", err)
	}
	body := defaultBody(name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return Ref{}, fmt.Errorf("write template: %v", err)
	}
	return Ref{ID: id, Name: name, Path: path, ContentHash: contentHash(body)}, nil
}

func defaultBody(name string) string {
	return fmt.Sprintf("<!-- track-template\nname: %s\n-->\n# {{ title }}\n", name)
}

func freeID(cfg *config.Config, t time.Time) (int64, error) {
	for id := t.Unix() * 1000; ; id++ {
		_, err := os.Stat(cfg.TemplatePath(id))
		if os.IsNotExist(err) {
			return id, nil
		}
		if err != nil {
			return 0, err
		}
	}
}

// List returns every user template in the vault, sorted by name.
func List(cfg *config.Config) ([]Ref, error) {
	entries, err := os.ReadDir(cfg.TemplateDir())
	if err != nil {
		if os.IsNotExist(err) {
			return []Ref{}, nil
		}
		return nil, err
	}
	var out []Ref
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".template"+cfg.PrimaryExt()) {
			continue
		}
		id, err := idFromName(entry.Name(), cfg)
		if err != nil {
			continue
		}
		t, err := read(cfg.TemplatePath(id))
		if err != nil {
			return nil, err
		}
		out = append(out, t.ref)
	}
	slices.SortFunc(out, func(a, b Ref) int {
		return strings.Compare(a.Name, b.Name)
	})
	return out, nil
}

// FindByName resolves a user template by its directive name. It errors when the name is ambiguous.
func FindByName(cfg *config.Config, name string) (Ref, bool, error) {
	templates, err := List(cfg)
	if err != nil {
		return Ref{}, false, err
	}
	var found *Ref
	for i := range templates {
		if templates[i].Name != name {
			continue
		}
		if found != nil {
			return Ref{}, false, fmt.Errorf("template name %q is ambiguous", name)
		}
		found = &templates[i]
	}
	if found == nil {
		return Ref{}, false, nil
	}
	return *found, true, nil
}

// DefaultSpec returns the template to apply when a note/journal is created without an explicit template
// (and without an inline body). A configured default wins (config default_template / journal_template, or
// the matching env override); otherwise a template literally named "default" (notes) or "journal"
// (journals) is used when one exists, resolving a user template first and then the builtin shipped with
// track. It returns "" only when no template of that name resolves at all.
func DefaultSpec(cfg *config.Config, kind string) (string, error) {
	configured := cfg.DefaultTemplate
	reserved := "default"
	if kind == config.KindJournal {
		configured = cfg.JournalTemplate
		reserved = "journal"
	}
	if strings.TrimSpace(configured) != "" {
		return strings.TrimSpace(configured), nil
	}
	if found, err := nameExists(cfg, reserved); err != nil {
		return "", err
	} else if found {
		return reserved, nil
	}
	return "", nil
}

func nameExists(cfg *config.Config, name string) (bool, error) {
	if _, found, err := FindByName(cfg, name); err != nil || found {
		return found, err
	}
	_, found, err := builtinByName(name)
	return found, err
}

func resolve(cfg *config.Config, spec string) (data, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return data{}, fmt.Errorf("template is required")
	}
	if looksLikePath(spec, cfg) {
		path := spec
		if !filepath.IsAbs(path) {
			path = filepath.Join(cfg.VaultDir, path)
		}
		return read(path)
	}
	ref, found, err := FindByName(cfg, spec)
	if err != nil {
		return data{}, err
	}
	if found {
		return read(ref.Path)
	}
	// Fall back to a builtin shipped with track. A user template of the same name was resolved above,
	// so it takes precedence.
	if t, found, err := builtinByName(spec); err != nil {
		return data{}, err
	} else if found {
		return t, nil
	}
	return data{}, fmt.Errorf("template %q not found", spec)
}

func looksLikePath(spec string, cfg *config.Config) bool {
	return strings.ContainsAny(spec, `/\`) || strings.HasSuffix(spec, ".template"+cfg.PrimaryExt())
}

func read(path string) (data, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return data{}, err
	}
	body := string(raw)
	id, err := idFromName(filepath.Base(path), &config.Config{Extensions: []string{filepath.Ext(path)}})
	if err != nil {
		return data{}, err
	}
	name, content, err := splitDirective(body)
	if err != nil {
		return data{}, fmt.Errorf("%s: %w", path, err)
	}
	return data{
		ref:  Ref{ID: id, Name: name, Path: path, ContentHash: contentHash(body)},
		body: content,
	}, nil
}

func idFromName(name string, cfg *config.Config) (int64, error) {
	suffix := ".template" + cfg.PrimaryExt()
	stem := strings.TrimSuffix(name, suffix)
	if stem == name {
		return 0, fmt.Errorf("not a template filename: %s", name)
	}
	return strconv.ParseInt(stem, 10, 64)
}

func splitDirective(body string) (name string, content string, err error) {
	const open = "<!-- track-template"
	const close = "-->"
	if !strings.HasPrefix(body, open) {
		return "", "", fmt.Errorf("missing track-template directive")
	}
	rest := strings.TrimPrefix(body, open)
	i := strings.Index(rest, close)
	if i < 0 {
		return "", "", fmt.Errorf("unterminated track-template directive")
	}
	for _, line := range strings.Split(rest[:i], "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		if strings.TrimSpace(key) == "name" {
			name = strings.TrimSpace(value)
		}
	}
	if name == "" {
		return "", "", fmt.Errorf("track-template directive requires name")
	}
	return name, strings.TrimLeft(rest[i+len(close):], "\r\n"), nil
}

var templateVar = regexp.MustCompile(`\{\{\s*(title|id|date|kind|parent)\s*\}\}`)

// Render expands a template body. parent is the title of the note the creation was triggered from (e.g.
// an action link's source note); it is empty when there is no such context, in which case {{ parent }}
// renders as an empty string.
func Render(cfg *config.Config, spec string, title string, id int64, kind, parent string, now time.Time) (string, error) {
	t, err := resolve(cfg, spec)
	if err != nil {
		return "", err
	}
	values := map[string]string{
		"title":  title,
		"id":     strconv.FormatInt(id, 10),
		"date":   now.Format(cfg.DateFormat),
		"kind":   kind,
		"parent": parent,
	}
	rendered := templateVar.ReplaceAllStringFunc(t.body, func(match string) string {
		key := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(match, "{{"), "}}"))
		return values[key]
	})
	return rendered, nil
}

func contentHash(body string) string {
	sum := sha256.Sum256([]byte(body))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// builtinByName returns the shipped builtin whose directive name matches.
func builtinByName(name string) (data, bool, error) {
	templates, err := builtins()
	if err != nil {
		return data{}, false, err
	}
	for _, t := range templates {
		if t.ref.Name == name {
			return t, true, nil
		}
	}
	return data{}, false, nil
}

// builtins parses every embedded builtin template. A builtin has no vault file, so its ref carries id 0,
// Builtin=true, and a "builtin:<name>" marker path.
func builtins() ([]data, error) {
	entries, err := builtin.Templates.ReadDir(".")
	if err != nil {
		return nil, err
	}
	var out []data
	for _, entry := range entries {
		raw, err := builtin.Templates.ReadFile(entry.Name())
		if err != nil {
			return nil, err
		}
		body := string(raw)
		name, content, err := splitDirective(body)
		if err != nil {
			return nil, fmt.Errorf("builtin %s: %w", entry.Name(), err)
		}
		out = append(out, data{
			ref:  Ref{Name: name, Path: "builtin:" + name, ContentHash: contentHash(body), Builtin: true},
			body: content,
		})
	}
	return out, nil
}

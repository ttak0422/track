package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/track/config"
)

type templateRef struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Path        string `json:"path"`
	ContentHash string `json:"content_hash"`
}

type templateData struct {
	ref  templateRef
	body string
}

func cmdTemplate(args []string) int {
	if len(args) == 0 {
		return fail("template subcommand is required: new, open, list")
	}
	switch args[0] {
	case "new":
		return cmdTemplateNew(args[1:])
	case "open":
		return cmdTemplateOpen(args[1:])
	case "list":
		return cmdTemplateList(args[1:])
	default:
		return fail("unknown template subcommand %q", args[0])
	}
}

func cmdTemplateNew(args []string) int {
	fs := flagSet("template new")
	name := fs.String("name", "", "template name")
	id := fs.Int64("id", 0, "template id; defaults to current Unix second * 1000 plus a same-second sequence")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		return fail("%v", err)
	}
	t, err := createTemplate(cfg, strings.TrimSpace(*name), *id, false)
	if err != nil {
		return fail("%v", err)
	}
	return emit(map[string]any{"id": t.ID, "name": t.Name, "path": t.Path, "created": true})
}

func cmdTemplateOpen(args []string) int {
	fs := flagSet("template open")
	name := fs.String("name", "", "template name")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		return fail("%v", err)
	}
	n := strings.TrimSpace(*name)
	if n == "" {
		return fail("--name is required")
	}
	if t, found, err := findTemplateByName(cfg, n); err != nil {
		return fail("find template: %v", err)
	} else if found {
		return emit(map[string]any{"id": t.ID, "name": t.Name, "path": t.Path, "created": false})
	}
	t, err := createTemplate(cfg, n, 0, false)
	if err != nil {
		return fail("%v", err)
	}
	return emit(map[string]any{"id": t.ID, "name": t.Name, "path": t.Path, "created": true})
}

func cmdTemplateList(args []string) int {
	fs := flagSet("template list")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		return fail("%v", err)
	}
	templates, err := listTemplates(cfg)
	if err != nil {
		return fail("list templates: %v", err)
	}
	if templates == nil {
		templates = []templateRef{}
	}
	return emit(map[string]any{"templates": templates})
}

func flagSet(name string) *flag.FlagSet {
	return flag.NewFlagSet(name, flag.ContinueOnError)
}

func createTemplate(cfg *config.Config, name string, id int64, overwrite bool) (templateRef, error) {
	if name == "" {
		return templateRef{}, fmt.Errorf("--name is required")
	}
	if _, found, err := findTemplateByName(cfg, name); err != nil {
		return templateRef{}, err
	} else if found {
		return templateRef{}, fmt.Errorf("template already exists for name %q", name)
	}
	var err error
	if id == 0 {
		id, err = freeTemplateID(cfg, time.Now())
		if err != nil {
			return templateRef{}, err
		}
	}
	path := cfg.TemplatePath(id)
	if !overwrite {
		if _, err := os.Stat(path); err == nil {
			return templateRef{}, fmt.Errorf("template already exists: %s", path)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return templateRef{}, fmt.Errorf("create template dir: %v", err)
	}
	body := defaultTemplateBody(name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return templateRef{}, fmt.Errorf("write template: %v", err)
	}
	return templateRef{ID: id, Name: name, Path: path, ContentHash: contentHash(body)}, nil
}

func defaultTemplateBody(name string) string {
	return fmt.Sprintf("<!-- track-template\nname: %s\n-->\n# {{ title }}\n", name)
}

func freeTemplateID(cfg *config.Config, t time.Time) (int64, error) {
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

func listTemplates(cfg *config.Config) ([]templateRef, error) {
	entries, err := os.ReadDir(cfg.TemplateDir())
	if err != nil {
		if os.IsNotExist(err) {
			return []templateRef{}, nil
		}
		return nil, err
	}
	var out []templateRef
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".template"+cfg.PrimaryExt()) {
			continue
		}
		id, err := templateIDFromName(entry.Name(), cfg)
		if err != nil {
			continue
		}
		t, err := readTemplate(cfg.TemplatePath(id))
		if err != nil {
			return nil, err
		}
		out = append(out, t.ref)
	}
	slices.SortFunc(out, func(a, b templateRef) int {
		return strings.Compare(a.Name, b.Name)
	})
	return out, nil
}

func findTemplateByName(cfg *config.Config, name string) (templateRef, bool, error) {
	templates, err := listTemplates(cfg)
	if err != nil {
		return templateRef{}, false, err
	}
	var found *templateRef
	for i := range templates {
		if templates[i].Name != name {
			continue
		}
		if found != nil {
			return templateRef{}, false, fmt.Errorf("template name %q is ambiguous", name)
		}
		found = &templates[i]
	}
	if found == nil {
		return templateRef{}, false, nil
	}
	return *found, true, nil
}

func resolveTemplate(cfg *config.Config, spec string) (templateData, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return templateData{}, fmt.Errorf("template is required")
	}
	if looksLikeTemplatePath(spec, cfg) {
		path := spec
		if !filepath.IsAbs(path) {
			path = filepath.Join(cfg.VaultDir, path)
		}
		return readTemplate(path)
	}
	ref, found, err := findTemplateByName(cfg, spec)
	if err != nil {
		return templateData{}, err
	}
	if !found {
		return templateData{}, fmt.Errorf("template %q not found", spec)
	}
	return readTemplate(ref.Path)
}

func looksLikeTemplatePath(spec string, cfg *config.Config) bool {
	return strings.ContainsAny(spec, `/\`) || strings.HasSuffix(spec, ".template"+cfg.PrimaryExt())
}

func readTemplate(path string) (templateData, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return templateData{}, err
	}
	body := string(raw)
	id, err := templateIDFromName(filepath.Base(path), &config.Config{Extensions: []string{filepath.Ext(path)}})
	if err != nil {
		return templateData{}, err
	}
	name, content, err := splitTemplateDirective(body)
	if err != nil {
		return templateData{}, fmt.Errorf("%s: %w", path, err)
	}
	return templateData{
		ref:  templateRef{ID: id, Name: name, Path: path, ContentHash: contentHash(body)},
		body: content,
	}, nil
}

func templateIDFromName(name string, cfg *config.Config) (int64, error) {
	suffix := ".template" + cfg.PrimaryExt()
	stem := strings.TrimSuffix(name, suffix)
	if stem == name {
		return 0, fmt.Errorf("not a template filename: %s", name)
	}
	return strconv.ParseInt(stem, 10, 64)
}

func splitTemplateDirective(body string) (name string, content string, err error) {
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

var templateVar = regexp.MustCompile(`\{\{\s*(title|id|date|kind)\s*\}\}`)

func renderTemplate(cfg *config.Config, templateSpec string, title string, id int64, kind string, now time.Time) (string, error) {
	t, err := resolveTemplate(cfg, templateSpec)
	if err != nil {
		return "", err
	}
	values := map[string]string{
		"title": title,
		"id":    strconv.FormatInt(id, 10),
		"date":  now.Format(cfg.DateFormat),
		"kind":  kind,
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

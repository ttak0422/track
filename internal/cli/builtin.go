package cli

import (
	"embed"
	"fmt"
)

// builtinTemplateFS holds the templates shipped with track itself (provided by the repository, not the
// vault). They are read straight from the binary; a user template of the same name in template/ is
// resolved first, so creating one overrides the builtin.
//
//go:embed builtin_templates/*.template.md
var builtinTemplateFS embed.FS

const builtinTemplateSrcDir = "builtin_templates"

// builtinTemplateByName returns the shipped builtin whose directive name matches.
func builtinTemplateByName(name string) (templateData, bool, error) {
	templates, err := builtinTemplates()
	if err != nil {
		return templateData{}, false, err
	}
	for _, t := range templates {
		if t.ref.Name == name {
			return t, true, nil
		}
	}
	return templateData{}, false, nil
}

// builtinTemplates parses every embedded builtin template. A builtin has no vault file, so its ref
// carries id 0, Builtin=true, and a "builtin:<name>" marker path.
func builtinTemplates() ([]templateData, error) {
	entries, err := builtinTemplateFS.ReadDir(builtinTemplateSrcDir)
	if err != nil {
		return nil, err
	}
	var out []templateData
	for _, entry := range entries {
		raw, err := builtinTemplateFS.ReadFile(builtinTemplateSrcDir + "/" + entry.Name())
		if err != nil {
			return nil, err
		}
		body := string(raw)
		name, content, err := splitTemplateDirective(body)
		if err != nil {
			return nil, fmt.Errorf("builtin %s: %w", entry.Name(), err)
		}
		out = append(out, templateData{
			ref:  templateRef{Name: name, Path: "builtin:" + name, ContentHash: contentHash(body), Builtin: true},
			body: content,
		})
	}
	return out, nil
}

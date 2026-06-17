package cli

import (
	"fmt"

	"github.com/ttak0422/track/builtin"
)

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
	entries, err := builtin.Templates.ReadDir(".")
	if err != nil {
		return nil, err
	}
	var out []templateData
	for _, entry := range entries {
		raw, err := builtin.Templates.ReadFile(entry.Name())
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

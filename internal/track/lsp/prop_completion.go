package lsp

import (
	"regexp"
	"sort"
	"strings"

	"github.com/ttak0422/track/internal/track/note"
	protocol "typefox.dev/lsp"
)

// Property completion offers the keys declared in the config `properties:` schema and, after
// "key:: ", that key's enum candidates (or true/false for a boolean). It only activates when a
// schema is configured, so unconfigured vaults see no extra noise.

var (
	// propValueCtxRe finds an inline field's key and typed value fragment at the end of the line
	// prefix: "status::" or "[status:: dra". The value fragment stops at "]" so the bracketed form
	// [key:: value] completes too.
	propValueCtxRe = regexp.MustCompile(`([A-Za-z][A-Za-z0-9_-]*)::([ \t]*)([^\]]*)$`)
	// propKeyCtxRe matches a bare word being typed where a field can start: at the line start after
	// an optional list marker, or right after "[".
	propKeyCtxRe = regexp.MustCompile(`(?:^\s*(?:[-*+]\s+|\d+[.)]\s+)?|\[)([A-Za-z][A-Za-z0-9_-]*)$`)
)

func (s *Server) propertyCompletion(text string, pos position) ([]completionItem, bool) {
	if len(s.cfg.Properties) == 0 {
		return nil, false
	}
	lines := strings.Split(text, "\n")
	lineNo := int(pos.Line)
	if lineNo >= len(lines) || inFencedBlock(lines, lineNo) {
		return nil, false
	}
	line := lines[lineNo]
	col := min(int(pos.Character), len(line))
	prefix := line[:col]

	if m := propValueCtxRe.FindStringSubmatchIndex(prefix); m != nil {
		key := prefix[m[2]:m[3]]
		spec, ok := s.cfg.Properties[key]
		if !ok {
			return nil, false
		}
		candidates := spec.Values
		if len(candidates) == 0 && spec.Type == note.TypeBoolean {
			candidates = []string{"true", "false"}
		}
		if len(candidates) == 0 {
			return nil, false
		}
		// Replace everything after "::" so the edit normalizes to exactly one separating space.
		typed := strings.TrimSpace(prefix[m[6]:m[7]])
		items := make([]completionItem, 0, len(candidates))
		for _, value := range candidates {
			if typed != "" && !strings.HasPrefix(strings.ToLower(value), strings.ToLower(typed)) {
				continue
			}
			items = append(items, completionItem{
				Label:      value,
				Kind:       protocol.ValueCompletion,
				Detail:     "property value",
				InsertText: value,
				FilterText: value,
				TextEdit:   plainCompletionTextEdit(lineNo, m[4], col, " "+value),
			})
		}
		return items, true
	}

	if m := propKeyCtxRe.FindStringSubmatchIndex(prefix); m != nil {
		typed := prefix[m[2]:m[3]]
		keys := make([]string, 0, len(s.cfg.Properties))
		for key := range s.cfg.Properties {
			if strings.HasPrefix(strings.ToLower(key), strings.ToLower(typed)) {
				keys = append(keys, key)
			}
		}
		if len(keys) == 0 {
			return nil, false
		}
		sort.Strings(keys)
		items := make([]completionItem, 0, len(keys))
		for _, key := range keys {
			spec := s.cfg.Properties[key]
			detail := "property"
			if spec.Type != "" {
				detail = "property (" + spec.Type + ")"
			}
			items = append(items, completionItem{
				Label:      key,
				Kind:       protocol.PropertyCompletion,
				Detail:     detail,
				InsertText: key,
				FilterText: key,
				TextEdit:   plainCompletionTextEdit(lineNo, m[2], col, key+":: "),
			})
		}
		return items, true
	}

	return nil, false
}

// inFencedBlock reports whether lineNo sits inside an open ``` / ~~~ code fence, so property
// completion never fires on source code that happens to look like a field.
func inFencedBlock(lines []string, lineNo int) bool {
	open := false
	for i := 0; i < lineNo && i < len(lines); i++ {
		trimmed := strings.TrimLeft(lines[i], " \t")
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			open = !open
		}
	}
	return open
}

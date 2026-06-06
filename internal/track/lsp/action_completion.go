package lsp

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	protocol "typefox.dev/lsp"
)

type actionCompletionContext struct {
	Line         int
	ReplaceStart int
	ReplaceEnd   int
	Mode         string
	Action       string
	Key          string
	Prefix       string
	UsedKeys     map[string]bool
}

var trackActions = map[string]bool{
	"journal": true,
	"new":     true,
	"note":    true,
	"open":    true,
	"today":   true,
}

func (s *Server) actionCompletion(text string, pos position) ([]completionItem, bool) {
	ctx, ok := actionCompletionContextAt(text, pos)
	if !ok {
		return nil, false
	}
	switch ctx.Mode {
	case "action":
		return actionCompletionItems(ctx, []actionCandidate{
			{Label: "open", Insert: "open?title={{date}} &template=", Doc: "Open or create a regular note from a title."},
			{Label: "journal", Insert: "journal?template=", Doc: "Open or create a journal note; template is used only when creating."},
			{Label: "today", Insert: "today?template=", Doc: "Open or create today's journal note."},
		}, protocol.SnippetCompletion), true
	case "param":
		return actionCompletionItems(ctx, actionParamCandidates(ctx), protocol.PropertyCompletion), true
	case "value":
		return s.actionValueCompletions(ctx), true
	}
	return []completionItem{}, true
}

type actionCandidate struct {
	Label  string
	Insert string
	Doc    string
}

func actionCompletionItems(ctx actionCompletionContext, candidates []actionCandidate, kind protocol.CompletionItemKind) []completionItem {
	items := make([]completionItem, 0, len(candidates))
	for _, candidate := range candidates {
		if ctx.Prefix != "" && !strings.HasPrefix(candidate.Label, ctx.Prefix) {
			continue
		}
		insert := candidate.Insert
		if insert == "" {
			insert = candidate.Label
		}
		item := completionItem{
			Label:    candidate.Label,
			Kind:     kind,
			Detail:   "track action",
			TextEdit: plainCompletionTextEdit(ctx.Line, ctx.ReplaceStart, ctx.ReplaceEnd, insert),
		}
		if candidate.Doc != "" {
			item.Documentation = markdownDocumentation(candidate.Doc)
		}
		items = append(items, item)
	}
	return items
}

func actionParamCandidates(ctx actionCompletionContext) []actionCandidate {
	var keys []string
	switch ctx.Action {
	case "journal", "today":
		keys = []string{"template", "offset", "date"}
	case "open", "new", "note":
		keys = []string{"title", "template"}
	default:
		keys = []string{"template"}
	}
	out := make([]actionCandidate, 0, len(keys))
	for _, key := range keys {
		if ctx.UsedKeys[key] {
			continue
		}
		out = append(out, actionCandidate{Label: key, Insert: key + "="})
	}
	return out
}

func (s *Server) actionValueCompletions(ctx actionCompletionContext) []completionItem {
	var candidates []actionCandidate
	switch ctx.Key {
	case "template":
		for _, name := range s.templateNames() {
			candidates = append(candidates, actionCandidate{Label: name})
		}
	case "title":
		candidates = []actionCandidate{
			{Label: "{{date}}", Insert: "{{date}} "},
			{Label: "{{journal}}", Insert: "{{journal}} "},
		}
	case "offset":
		candidates = []actionCandidate{{Label: "0"}, {Label: "-1"}, {Label: "1"}}
	case "date":
		candidates = []actionCandidate{{Label: "today"}, {Label: "yesterday"}, {Label: "tomorrow"}}
	}
	return actionCompletionItems(ctx, candidates, protocol.ValueCompletion)
}

func actionCompletionContextAt(text string, pos position) (actionCompletionContext, bool) {
	lines := strings.Split(text, "\n")
	lineNo := int(pos.Line)
	if lineNo >= len(lines) {
		return actionCompletionContext{}, false
	}
	line := lines[lineNo]
	col := min(int(pos.Character), len(line))

	searchFrom := 0
	for {
		i := strings.Index(line[searchFrom:], "](<")
		if i < 0 {
			return actionCompletionContext{}, false
		}
		angle := searchFrom + i + 2
		targetStart := angle + 1
		targetEnd := len(line)
		if closeAngle := strings.IndexByte(line[targetStart:], '>'); closeAngle >= 0 {
			targetEnd = targetStart + closeAngle
		}
		if col >= targetStart && col <= targetEnd {
			return actionCompletionContextForSpec(line[targetStart:col], lineNo, targetStart)
		}
		searchFrom = angle + 1
	}
}

func actionCompletionContextForSpec(spec string, lineNo, targetStart int) (actionCompletionContext, bool) {
	ctx := actionCompletionContext{Line: lineNo, UsedKeys: map[string]bool{}}
	firstSep := strings.IndexAny(spec, "?&")
	if firstSep < 0 {
		ctx.Mode = "action"
		ctx.Prefix = spec
		ctx.ReplaceStart = targetStart
		ctx.ReplaceEnd = targetStart + len(spec)
		return ctx, true
	}

	ctx.Action = strings.Trim(spec[:firstSep], "/")
	if !trackActions[ctx.Action] {
		return actionCompletionContext{}, false
	}
	query := spec[firstSep+1:]
	currentStart := 0
	if lastQ := strings.LastIndexAny(query, "?&"); lastQ >= 0 {
		currentStart = lastQ + 1
	}
	for _, segment := range strings.FieldsFunc(query[:currentStart], func(r rune) bool { return r == '&' || r == '?' }) {
		key, _, ok := strings.Cut(segment, "=")
		if ok && key != "" {
			ctx.UsedKeys[key] = true
		}
	}
	current := query[currentStart:]
	ctx.ReplaceStart = targetStart + firstSep + 1 + currentStart
	ctx.ReplaceEnd = targetStart + len(spec)
	if key, value, ok := strings.Cut(current, "="); ok {
		ctx.Mode = "value"
		ctx.Key = key
		ctx.Prefix = value
		ctx.ReplaceStart += len(key) + 1
		return ctx, true
	}
	ctx.Mode = "param"
	ctx.Prefix = current
	return ctx, true
}

func (s *Server) templateNames() []string {
	entries, err := os.ReadDir(s.cfg.TemplateDir())
	if err != nil {
		return nil
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".template"+s.cfg.PrimaryExt()) {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(s.cfg.TemplateDir(), entry.Name()))
		if err != nil {
			continue
		}
		if name := templateNameFromDirective(string(raw)); name != "" {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

func templateNameFromDirective(body string) string {
	const open = "<!-- track-template"
	const close = "-->"
	if !strings.HasPrefix(body, open) {
		return ""
	}
	rest := strings.TrimPrefix(body, open)
	i := strings.Index(rest, close)
	if i < 0 {
		return ""
	}
	for _, line := range strings.Split(rest[:i], "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if ok && strings.TrimSpace(key) == "name" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

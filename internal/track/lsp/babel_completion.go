package lsp

import (
	"slices"
	"sort"
	"strings"

	protocol "typefox.dev/lsp"
)

var babelHeaderKeys = []string{
	"name",
	"results",
	"eval",
	"cache",
	"var",
	"session",
	"dir",
	"exports",
	"noweb",
	"tangle",
}

var babelHeaderValues = map[string][]string{
	"results": {"output", "verbatim", "replace", "silent", "none", "discard", "raw", "code"},
	"eval":    {"yes", "no", "query"},
	"cache":   {"yes", "no"},
	"session": {"none"},
	"exports": {"code", "results", "both", "none"},
	"noweb":   {"no"},
	"tangle":  {"no"},
}

type babelCompletionContext struct {
	Line         int
	ReplaceStart int
	ReplaceEnd   int
	Mode         string
	Key          string
	HasKeyValue  bool
	Prefix       string
}

func (s *Server) babelCompletion(text string, pos position) []completionItem {
	ctx, ok := babelCompletionContextAt(text, pos)
	if !ok {
		return []completionItem{}
	}

	switch ctx.Mode {
	case "language":
		langs := make([]string, 0, len(s.cfg.BabelLanguages))
		for lang := range s.cfg.BabelLanguages {
			langs = append(langs, lang)
		}
		sort.Strings(langs)
		return babelCompletionItems(ctx, langs, protocol.KeywordCompletion, "babel language", "")
	case "key":
		keys := make([]string, 0, len(babelHeaderKeys))
		for _, key := range babelHeaderKeys {
			keys = append(keys, ":"+key)
		}
		return babelCompletionItems(ctx, keys, protocol.PropertyCompletion, "babel header", " ")
	case "value":
		if ctx.Prefix == "" && ctx.HasKeyValue {
			return s.babelHeaderKeyItems(ctx)
		}
		values := slices.Clone(babelHeaderValues[ctx.Key])
		return babelCompletionItems(ctx, values, protocol.ValueCompletion, ":"+ctx.Key, "")
	}
	return []completionItem{}
}

func (s *Server) babelHeaderKeyItems(ctx babelCompletionContext) []completionItem {
	keyCtx := ctx
	keyCtx.Mode = "key"
	keyCtx.Key = ""
	keys := make([]string, 0, len(babelHeaderKeys))
	for _, key := range babelHeaderKeys {
		keys = append(keys, ":"+key)
	}
	return babelCompletionItems(keyCtx, keys, protocol.PropertyCompletion, "babel header", " ")
}

func babelCompletionItems(ctx babelCompletionContext, candidates []string, kind protocol.CompletionItemKind, detail, suffix string) []completionItem {
	items := make([]completionItem, 0, len(candidates))
	for _, candidate := range candidates {
		if ctx.Prefix != "" && !strings.HasPrefix(candidate, ctx.Prefix) {
			continue
		}
		newText := candidate + suffix
		items = append(items, completionItem{
			Label:      candidate,
			Kind:       kind,
			Detail:     detail,
			InsertText: candidate,
			TextEdit:   plainCompletionTextEdit(ctx.Line, ctx.ReplaceStart, ctx.ReplaceEnd, newText),
		})
	}
	return items
}

func babelCompletionContextAt(text string, pos position) (babelCompletionContext, bool) {
	lines := strings.Split(text, "\n")
	lineNo := int(pos.Line)
	if lineNo >= len(lines) {
		return babelCompletionContext{}, false
	}
	line := lines[lineNo]
	col := min(int(pos.Character), len(line))
	fence := strings.Index(line, "```")
	if fence < 0 || strings.TrimSpace(line[:fence]) != "" || col < fence+3 {
		return babelCompletionContext{}, false
	}

	infoStart := fence + 3
	tokenStart := col
	for tokenStart > infoStart && !isSpace(line[tokenStart-1]) {
		tokenStart--
	}
	prefix := line[tokenStart:col]
	beforeCurrent := strings.TrimSpace(line[infoStart:tokenStart])
	tokens := strings.Fields(beforeCurrent)

	ctx := babelCompletionContext{
		Line:         lineNo,
		ReplaceStart: tokenStart,
		ReplaceEnd:   col,
		Prefix:       prefix,
	}

	if len(tokens) == 0 && !strings.HasPrefix(prefix, ":") {
		ctx.Mode = "language"
		return ctx, true
	}
	if strings.HasPrefix(prefix, ":") {
		ctx.Mode = "key"
		return ctx, true
	}
	if len(tokens) == 0 {
		ctx.Mode = "key"
		return ctx, true
	}

	activeKey := ""
	hasKeyValue := false
	for _, token := range tokens[1:] {
		if strings.HasPrefix(token, ":") {
			activeKey = strings.TrimPrefix(token, ":")
			hasKeyValue = false
			continue
		}
		if activeKey != "" {
			hasKeyValue = true
		}
	}
	if values := babelHeaderValues[activeKey]; len(values) > 0 {
		ctx.Mode = "value"
		ctx.Key = activeKey
		ctx.HasKeyValue = hasKeyValue
		return ctx, true
	}
	ctx.Mode = "key"
	return ctx, true
}

func plainCompletionTextEdit(line, start, end int, text string) *protocol.Or_CompletionItem_textEdit {
	return &protocol.Or_CompletionItem_textEdit{
		Value: textEdit{
			Range:   newRange(line, start, line, end),
			NewText: text,
		},
	}
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\t'
}

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
	"visible-lines",
}

var babelHeaderValues = map[string][]string{
	"results": {"output", "verbatim", "replace", "silent", "none", "discard", "raw", "code"},
	"eval":    {"yes", "no", "query"},
	"cache":   {"yes", "no"},
	"session": {"none"},
	"exports": {"code", "results", "both", "none"},
	"noweb":   {"no", "yes", "eval", "tangle"},
	"tangle":  {"no"},
}

var babelHeaderDocs = map[string]string{
	":name":          "Names the source block for stable result lookup, future calls, and noweb references. Names should be unique within a note.",
	":results":       "Controls how execution results are captured and stored in sidecar metadata instead of mutating the Markdown body.",
	":eval":          "Controls whether the block may execute: `yes` allows execution, `no` prevents execution, and `query` asks before running.",
	":cache":         "Controls result reuse with a cache key based on the block body, normalized header arguments, and variable references.",
	":var":           "Defines input variables such as `x=1`, injected into the block's process environment; a value naming another named block feeds that block's stored result.",
	":session":       "Controls interpreter session behavior. `none` runs without a long-lived session; named sessions are reserved for later support.",
	":dir":           "Sets the execution working directory. Relative paths resolve from the note directory or vault and are restricted to allowed roots.",
	":exports":       "Records export intent for future compatibility. Track does not currently provide an exporter.",
	":noweb":         "Controls expansion of `<<name>>` references: `yes` expands before execution and tangling, `eval`/`tangle` only in that phase, `no` never.",
	":tangle":        "Names the file `track babel tangle` writes this block to, resolved against the note directory and confined to the vault; `no` disables output.",
	":visible-lines": "Controls editor-only source display. Use 1-based block-body lines such as `4-5` or `4-5,8`; execution still uses the full source block.",
}

var babelHeaderValueDocs = map[string]map[string]string{
	"results": {
		"output":   "Capture stdout, stderr, and exit status in sidecar metadata.",
		"verbatim": "Store raw text without coercing the result display format.",
		"replace":  "Replace the last stored result for this block in metadata.",
		"silent":   "Execute without updating stored results; command output can still be transient.",
		"none":     "Execute without storing or displaying a result.",
		"discard":  "Execute and ignore the result completely.",
		"raw":      "Store a raw-result marker in metadata without inserting into the Markdown body.",
		"code":     "Store the result plus a code render-format marker in metadata.",
	},
	"eval": {
		"yes":   "Allow execution subject to Track's security policy.",
		"no":    "Never execute this block.",
		"query": "Ask before execution; non-interactive frontends should require explicit confirmation.",
	},
	"cache": {
		"yes": "Reuse results when the body hash, normalized header arguments, and variable references match.",
		"no":  "Do not reuse cached results.",
	},
	"session": {
		"none": "Run without a long-lived interpreter session; effectively one process per block.",
	},
	"exports": {
		"code":    "Record that code should be exported when exporter support exists.",
		"results": "Record that results should be exported when exporter support exists.",
		"both":    "Record that both code and results should be exported when exporter support exists.",
		"none":    "Record that neither code nor results should be exported when exporter support exists.",
	},
	"noweb": {
		"no":     "Do not expand `<<name>>` references.",
		"yes":    "Expand `<<name>>` references before execution and before tangling.",
		"eval":   "Expand `<<name>>` references only before execution.",
		"tangle": "Expand `<<name>>` references only before tangling.",
	},
	"tangle": {
		"no": "Do not write this block to an output file.",
	},
}

type babelCompletionContext struct {
	Line         int
	ReplaceStart int
	ReplaceEnd   int
	Mode         string
	Key          string
	HasKeyValue  bool
	UsedKeys     map[string]bool
	UsedValues   map[string]bool
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
		return babelCompletionItems(ctx, langs, protocol.KeywordCompletion, "babel language", "", nil)
	case "key":
		keys := make([]string, 0, len(babelHeaderKeys))
		for _, key := range babelHeaderKeys {
			if ctx.UsedKeys[key] {
				continue
			}
			keys = append(keys, ":"+key)
		}
		return babelCompletionItems(ctx, keys, protocol.PropertyCompletion, "babel header", " ", babelHeaderDoc)
	case "value":
		values := slices.Clone(babelHeaderValues[ctx.Key])
		if ctx.Key == "results" {
			values = unusedBabelValues(values, ctx.UsedValues)
			items := babelCompletionItems(ctx, values, protocol.ValueCompletion, ":"+ctx.Key, "", babelHeaderValueDoc(ctx.Key))
			if ctx.Prefix == "" && ctx.HasKeyValue {
				items = append(items, s.babelHeaderKeyItems(ctx)...)
			}
			return items
		}
		if ctx.Prefix == "" && ctx.HasKeyValue {
			return s.babelHeaderKeyItems(ctx)
		}
		return babelCompletionItems(ctx, values, protocol.ValueCompletion, ":"+ctx.Key, "", babelHeaderValueDoc(ctx.Key))
	}
	return []completionItem{}
}

func (s *Server) babelHeaderKeyItems(ctx babelCompletionContext) []completionItem {
	keyCtx := ctx
	keyCtx.Mode = "key"
	keyCtx.Key = ""
	keys := make([]string, 0, len(babelHeaderKeys))
	for _, key := range babelHeaderKeys {
		if ctx.UsedKeys[key] {
			continue
		}
		keys = append(keys, ":"+key)
	}
	return babelCompletionItems(keyCtx, keys, protocol.PropertyCompletion, "babel header", " ", babelHeaderDoc)
}

func babelCompletionItems(ctx babelCompletionContext, candidates []string, kind protocol.CompletionItemKind, detail, suffix string, doc func(string) string) []completionItem {
	items := make([]completionItem, 0, len(candidates))
	for _, candidate := range candidates {
		if ctx.Prefix != "" && !strings.HasPrefix(candidate, ctx.Prefix) {
			continue
		}
		newText := candidate + suffix
		item := completionItem{
			Label:      candidate,
			Kind:       kind,
			Detail:     detail,
			InsertText: candidate,
			TextEdit:   plainCompletionTextEdit(ctx.Line, ctx.ReplaceStart, ctx.ReplaceEnd, newText),
		}
		if doc != nil {
			item.Documentation = markdownDocumentation(doc(candidate))
		}
		items = append(items, item)
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
		UsedKeys:     usedBabelHeaderKeys(tokens),
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
	usedValues := map[string]bool{}
	for _, token := range tokens[1:] {
		if strings.HasPrefix(token, ":") {
			activeKey = strings.TrimPrefix(token, ":")
			hasKeyValue = false
			usedValues = map[string]bool{}
			continue
		}
		if activeKey != "" {
			hasKeyValue = true
			usedValues[token] = true
		}
	}
	if values := babelHeaderValues[activeKey]; len(values) > 0 {
		ctx.Mode = "value"
		ctx.Key = activeKey
		ctx.HasKeyValue = hasKeyValue
		ctx.UsedValues = usedValues
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

func markdownDocumentation(text string) *protocol.Or_CompletionItem_documentation {
	if text == "" {
		return nil
	}
	return &protocol.Or_CompletionItem_documentation{
		Value: protocol.MarkupContent{
			Kind:  protocol.Markdown,
			Value: text,
		},
	}
}

func babelHeaderDoc(candidate string) string {
	return babelHeaderDocs[candidate]
}

func babelHeaderValueDoc(key string) func(string) string {
	return func(candidate string) string {
		return babelHeaderValueDocs[key][candidate]
	}
}

func usedBabelHeaderKeys(tokens []string) map[string]bool {
	used := make(map[string]bool)
	if len(tokens) < 2 {
		return used
	}
	for _, token := range tokens[1:] {
		if strings.HasPrefix(token, ":") {
			used[strings.TrimPrefix(token, ":")] = true
		}
	}
	return used
}

func unusedBabelValues(values []string, used map[string]bool) []string {
	if len(used) == 0 {
		return values
	}
	unused := values[:0]
	for _, value := range values {
		if used[value] {
			continue
		}
		unused = append(unused, value)
	}
	return unused
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\t'
}

package lsp

import (
	"fmt"
	"strings"

	"github.com/ttak0422/track/internal/track/link"
	protocol "typefox.dev/lsp"
)

// completion offers note titles when the cursor sits inside an unclosed [[ on the current line.
// Existing candidates come from the same dictionary that resolves links. If the typed target has no
// matching keyword, an extra item lets the client create a note from that input.
func (s *Server) completion(uri string, pos position) ([]completionItem, error) {
	if !s.inVault(uri) {
		return []completionItem{}, nil
	}
	text, err := s.documentText(uri)
	if err != nil {
		return nil, err
	}
	if items, ok := s.actionCompletion(text, pos); ok {
		return items, nil
	}
	ctx, ok := openLinkCompletionContext(text, pos)
	if !ok {
		// Property fields sit outside [[links]]; inside an open [[ the link dictionary wins, so a
		// link-typed property value still completes note titles.
		if items, ok := s.propertyCompletion(text, pos); ok {
			return items, nil
		}
		return s.babelCompletion(text, pos), nil
	}
	if strings.Contains(ctx.Target, "#") {
		return s.headingCompletion(ctx)
	}
	currentID, hasCurrentID := noteIDFromURI(uri)
	kws, err := s.store.Keywords()
	if err != nil {
		return nil, err
	}
	items := make([]completionItem, 0, len(kws))
	hasPrefixMatch := false
	lowerTarget := strings.ToLower(ctx.Target)
	for _, kw := range kws {
		prefixMatch := ctx.Target != "" && strings.HasPrefix(strings.ToLower(kw.Term), lowerTarget)
		if prefixMatch {
			hasPrefixMatch = true
		}
		if hasCurrentID && kw.NoteID == currentID {
			continue
		}
		items = append(items, completionItem{
			Label:      kw.Term,
			Kind:       protocol.ReferenceCompletion,
			Detail:     kw.Kind,
			InsertText: kw.Term,
			TextEdit:   completionTextEdit(ctx, kw.Term),
		})
		// Once the user has started typing a note name, surface that note's headings as full
		// [[note##heading]] anchors next to the bare note, so jumping to a section needs no extra "#".
		if prefixMatch && kw.Kind == "title" {
			items = append(items, s.headingAnchorItems(ctx, kw.Term, s.notePath(kw.FileKind, kw.NoteID))...)
		}
	}
	if ctx.Target != "" && !hasPrefixMatch {
		items = append(items, createNoteCompletionItem(uri, ctx))
	}
	return items, nil
}

// headingAnchorItems offers a note's headings as full "note##heading" anchor completions for the
// pre-"#" stage, where the user has typed (part of) the note name but no "#" yet. A note whose body
// cannot be read (e.g. not yet on disk) contributes nothing.
func (s *Server) headingAnchorItems(ctx openLinkContext, term, path string) []completionItem {
	body, err := s.documentText(uriFromPath(path))
	if err != nil {
		return nil
	}
	var items []completionItem
	seen := map[string]bool{}
	for _, h := range link.Headings(body) {
		target := term + strings.Repeat("#", h.Level) + h.Text
		// Duplicate headings resolve to their first occurrence, so later twins are unreachable noise.
		if seen[target] {
			continue
		}
		seen[target] = true
		items = append(items, completionItem{
			Label:      target,
			Kind:       protocol.ReferenceCompletion,
			Detail:     fmt.Sprintf("h%d", h.Level),
			InsertText: target,
			FilterText: target,
			TextEdit:   completionTextEdit(ctx, target),
		})
	}
	return items
}

// headingCompletion offers a note's headings while the cursor sits inside an open [[note# ... ]].
// The note key before "#" is resolved against the keyword dictionary; matching headings (by the
// already-typed level and a prefix on the text) are offered, inserting the full [[note##heading]] target.
func (s *Server) headingCompletion(ctx openLinkContext) ([]completionItem, error) {
	key, level, prefix := splitHeadingPrefix(ctx.Target)
	if key == "" {
		return []completionItem{}, nil
	}
	dict, err := s.keywordDict()
	if err != nil {
		return nil, err
	}
	kw, ok := dict[key]
	if !ok {
		return []completionItem{}, nil
	}
	text, err := s.documentText(uriFromPath(s.notePath(kw.FileKind, kw.NoteID)))
	if err != nil {
		return nil, err
	}
	lowerPrefix := strings.ToLower(prefix)
	items := make([]completionItem, 0)
	seen := map[string]bool{}
	for _, h := range link.Headings(text) {
		if h.Level != level {
			continue
		}
		if prefix != "" && !strings.HasPrefix(strings.ToLower(h.Text), lowerPrefix) {
			continue
		}
		// Duplicate headings resolve to their first occurrence, so later twins are unreachable noise.
		if seen[h.Text] {
			continue
		}
		seen[h.Text] = true
		target := key + strings.Repeat("#", level) + h.Text
		items = append(items, completionItem{
			Label:      h.Text,
			Kind:       protocol.ReferenceCompletion,
			Detail:     fmt.Sprintf("h%d", h.Level),
			InsertText: target,
			TextEdit:   completionTextEdit(ctx, target),
		})
	}
	return items, nil
}

// splitHeadingPrefix parses an in-progress link target ("note#part") into the note key, the typed
// heading level (the run of "#"), and the partial heading text after it.
func splitHeadingPrefix(target string) (key string, level int, prefix string) {
	i := strings.IndexByte(target, '#')
	key = strings.TrimSpace(target[:i])
	rest := target[i:]
	for level < len(rest) && rest[level] == '#' {
		level++
	}
	prefix = strings.TrimSpace(rest[level:])
	return key, level, prefix
}

// insideOpenLink reports whether pos sits after a "[[" with no closing "]]" before it on the same line.
func insideOpenLink(text string, pos position) bool {
	lines := strings.Split(text, "\n")
	lineNo := int(pos.Line)
	if lineNo >= len(lines) {
		return false
	}
	line := lines[lineNo]
	col := int(pos.Character)
	if col > len(line) {
		col = len(line)
	}
	prefix := line[:col]
	open := strings.LastIndex(prefix, "[[")
	if open < 0 {
		return false
	}
	return !strings.Contains(prefix[open+2:], "]]")
}

type openLinkContext struct {
	Line         int
	ReplaceStart int
	ReplaceEnd   int
	NeedsClose   bool
	Target       string
}

func openLinkCompletionContext(text string, pos position) (openLinkContext, bool) {
	lines := strings.Split(text, "\n")
	lineNo := int(pos.Line)
	if lineNo >= len(lines) {
		return openLinkContext{}, false
	}
	line := lines[lineNo]
	col := int(pos.Character)
	if col > len(line) {
		col = len(line)
	}
	prefix := line[:col]
	open := strings.LastIndex(prefix, "[[")
	if open < 0 || strings.Contains(prefix[open+2:], "]]") {
		return openLinkContext{}, false
	}
	typed := prefix[open+2:]
	if strings.Contains(typed, "|") {
		return openLinkContext{}, false
	}
	closeAfterOpen := strings.Index(line[open+2:], "]]")
	needsClose := closeAfterOpen < 0 || open+2+closeAfterOpen < col
	return openLinkContext{
		Line:         lineNo,
		ReplaceStart: open + 2,
		ReplaceEnd:   col,
		NeedsClose:   needsClose,
		Target:       strings.TrimSpace(typed),
	}, true
}

func completionTextEdit(ctx openLinkContext, text string) *protocol.Or_CompletionItem_textEdit {
	newText := text
	if ctx.NeedsClose {
		newText += "]]"
	}
	return &protocol.Or_CompletionItem_textEdit{
		Value: textEdit{
			Range:   newRange(ctx.Line, ctx.ReplaceStart, ctx.Line, ctx.ReplaceEnd),
			NewText: newText,
		},
	}
}

func createNoteCompletionItem(uri string, ctx openLinkContext) completionItem {
	return completionItem{
		Label:      ctx.Target,
		Kind:       protocol.ReferenceCompletion,
		Detail:     "create note",
		InsertText: ctx.Target,
		FilterText: ctx.Target,
		SortText:   ctx.Target,
		TextEdit:   completionTextEdit(ctx, ctx.Target),
		Command:    createNoteLSPCommand(ctx.Target, uri),
	}
}

package lsp

import (
	"fmt"
	"strings"

	"github.com/ttak0422/track/internal/track/link"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

func (s *Server) documentLinks(uri string) ([]documentLink, error) {
	text, err := s.documentText(uri)
	if err != nil {
		return nil, err
	}
	currentID, hasCurrentID := noteIDFromURI(uri)
	dict, err := s.keywordDict()
	if err != nil {
		return nil, err
	}
	var links []documentLink
	for _, ref := range link.Refs(text) {
		kw, ok := dict[ref.Text]
		if !ok {
			continue // unresolved [[...]]: the Lua side highlights these separately
		}
		if hasCurrentID && kw.NoteID == currentID {
			continue
		}
		links = append(links, documentLink{
			Range: rangeValue{
				Start: position{Line: ref.Line, Character: ref.StartByte},
				End:   position{Line: ref.Line, Character: ref.EndByte},
			},
			Target:  uriFromPath(kw.Path),
			Tooltip: ref.Text,
		})
	}
	if links == nil {
		links = []documentLink{}
	}
	return links, nil
}

func (s *Server) definition(uri string, pos position) (*location, error) {
	text, err := s.documentText(uri)
	if err != nil {
		return nil, err
	}
	currentID, hasCurrentID := noteIDFromURI(uri)
	dict, err := s.keywordDict()
	if err != nil {
		return nil, err
	}
	for _, ref := range link.Refs(text) {
		if !refContainsPosition(ref, pos) {
			continue
		}
		kw, ok := dict[ref.Text]
		if !ok {
			return nil, nil
		}
		if hasCurrentID && kw.NoteID == currentID {
			return nil, nil
		}
		return &location{
			URI: uriFromPath(kw.Path),
			Range: rangeValue{
				Start: position{Line: 0, Character: 0},
				End:   position{Line: 0, Character: 0},
			},
		}, nil
	}
	return nil, nil
}

func refContainsPosition(ref link.Ref, pos position) bool {
	return ref.Line == pos.Line && pos.Character >= ref.OpenByte && pos.Character < ref.CloseByte
}

// completion offers note titles and aliases when the cursor sits inside an unclosed [[ on the current line.
// Existing candidates come from the same dictionary that resolves links. If the typed target has no
// matching keyword, an extra item lets the client create a note from that input.
func (s *Server) completion(uri string, pos position) ([]completionItem, error) {
	text, err := s.documentText(uri)
	if err != nil {
		return nil, err
	}
	ctx, ok := openLinkCompletionContext(text, pos)
	if !ok {
		return []completionItem{}, nil
	}
	currentID, hasCurrentID := noteIDFromURI(uri)
	kws, err := s.store.Keywords()
	if err != nil {
		return nil, err
	}
	items := make([]completionItem, 0, len(kws))
	hasPrefixMatch := false
	for _, kw := range kws {
		if ctx.Target != "" && strings.HasPrefix(strings.ToLower(kw.Term), strings.ToLower(ctx.Target)) {
			hasPrefixMatch = true
		}
		if hasCurrentID && kw.NoteID == currentID {
			continue
		}
		items = append(items, completionItem{
			Label:      kw.Term,
			Kind:       completionKindReference,
			Detail:     kw.Kind,
			InsertText: kw.Term,
			TextEdit:   completionTextEdit(ctx, kw.Term),
		})
	}
	if ctx.Target != "" && !hasPrefixMatch {
		items = append(items, createNoteCompletionItem(uri, ctx))
	}
	return items, nil
}

// insideOpenLink reports whether pos sits after a "[[" with no closing "]]" before it on the same line.
func insideOpenLink(text string, pos position) bool {
	lines := strings.Split(text, "\n")
	if pos.Line < 0 || pos.Line >= len(lines) {
		return false
	}
	line := lines[pos.Line]
	col := pos.Character
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
	if pos.Line < 0 || pos.Line >= len(lines) {
		return openLinkContext{}, false
	}
	line := lines[pos.Line]
	col := pos.Character
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
		Line:         pos.Line,
		ReplaceStart: open + 2,
		ReplaceEnd:   col,
		NeedsClose:   needsClose,
		Target:       strings.TrimSpace(typed),
	}, true
}

func completionTextEdit(ctx openLinkContext, text string) *textEdit {
	newText := text
	if ctx.NeedsClose {
		newText += "]]"
	}
	return &textEdit{
		Range: rangeValue{
			Start: position{Line: ctx.Line, Character: ctx.ReplaceStart},
			End:   position{Line: ctx.Line, Character: ctx.ReplaceEnd},
		},
		NewText: newText,
	}
}

func createNoteCompletionItem(uri string, ctx openLinkContext) completionItem {
	return completionItem{
		Label:      ctx.Target,
		Kind:       completionKindReference,
		Detail:     "create note",
		InsertText: ctx.Target,
		FilterText: ctx.Target,
		SortText:   ctx.Target,
		TextEdit:   completionTextEdit(ctx, ctx.Target),
		Command:    createNoteLSPCommand(ctx.Target, uri),
	}
}

func (s *Server) codeActions(uri string, rng rangeValue) ([]codeAction, error) {
	text, err := s.documentText(uri)
	if err != nil {
		return nil, err
	}
	dict, err := s.keywordDict()
	if err != nil {
		return nil, err
	}
	var actions []codeAction
	for _, ref := range link.Refs(text) {
		if _, ok := dict[ref.Text]; ok {
			continue
		}
		if !rangeTouchesRef(rng, ref) {
			continue
		}
		title := ref.Text
		actions = append(actions, codeAction{
			Title:   fmt.Sprintf("Create note %q", title),
			Kind:    "quickfix",
			Command: createNoteLSPCommand(title, uri),
		})
	}
	if actions == nil {
		actions = []codeAction{}
	}
	return actions, nil
}

func rangeTouchesRef(rng rangeValue, ref link.Ref) bool {
	if rng.Start.Line > ref.Line || rng.End.Line < ref.Line {
		return false
	}
	start := ref.OpenByte
	end := ref.CloseByte
	if rng.Start.Line == rng.End.Line && rng.Start.Character == rng.End.Character {
		return rng.Start.Line == ref.Line && rng.Start.Character >= start && rng.Start.Character <= end
	}
	rangeStart := 0
	if rng.Start.Line == ref.Line {
		rangeStart = rng.Start.Character
	}
	rangeEnd := end
	if rng.End.Line == ref.Line {
		rangeEnd = rng.End.Character
	}
	return rangeStart <= end && rangeEnd >= start
}

// keywordDict loads the auto-link dictionary keyed by term, so resolving each [[...]] is an O(1) lookup.
func (s *Server) keywordDict() (map[string]store.Keyword, error) {
	kws, err := s.store.Keywords()
	if err != nil {
		return nil, err
	}
	dict := make(map[string]store.Keyword, len(kws))
	for _, kw := range kws {
		if _, ok := dict[kw.Term]; !ok {
			dict[kw.Term] = kw
		}
	}
	return dict, nil
}

func noteIDFromURI(uri string) (int64, bool) {
	path, err := pathFromURI(uri)
	if err != nil {
		return 0, false
	}
	id, err := note.IDFromPath(path)
	return id, err == nil
}

package lsp

import (
	"fmt"
	"strings"

	"github.com/ttak0422/track/internal/track/link"
	"github.com/ttak0422/track/internal/track/note"
	trackrename "github.com/ttak0422/track/internal/track/rename"
	"github.com/ttak0422/track/internal/track/store"
	protocol "typefox.dev/lsp"
)

func (s *Server) documentLinks(uri string) ([]documentLink, error) {
	if !s.inVault(uri) {
		return []documentLink{}, nil
	}
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
		// A same-note link is inert unless it carries a heading anchor, which navigates within the note.
		if hasCurrentID && kw.NoteID == currentID && ref.Heading == "" {
			continue
		}
		target := protocol.URI(uriFromPath(s.notePath(kw.FileKind, kw.NoteID)))
		links = append(links, documentLink{
			Range:   newRange(ref.Line, ref.StartByte, ref.Line, ref.EndByte),
			Target:  &target,
			Tooltip: ref.Text,
		})
	}
	if links == nil {
		links = []documentLink{}
	}
	return links, nil
}

func (s *Server) backlinks(uri string) ([]backlink, error) {
	if !s.inVault(uri) {
		return []backlink{}, nil
	}
	currentID, ok := noteIDFromURI(uri)
	if !ok {
		return []backlink{}, nil
	}
	return s.backlinksTo(currentID)
}

func (s *Server) backlinksTo(noteID int64) ([]backlink, error) {
	sources, err := s.store.Backlinks(noteID)
	if err != nil {
		return nil, err
	}
	dict, err := s.keywordDict()
	if err != nil {
		return nil, err
	}

	var out []backlink
	for _, source := range sources {
		sourcePath := s.notePath(source.FileKind, source.NoteID)
		sourceURI := uriFromPath(sourcePath)
		text, err := s.documentText(sourceURI)
		if err != nil {
			return nil, err
		}
		lines := strings.Split(text, "\n")
		for _, ref := range link.Refs(text) {
			kw, ok := dict[ref.Text]
			if !ok || kw.NoteID != noteID {
				continue
			}
			preview := ""
			if ref.Line >= 0 && ref.Line < len(lines) {
				preview = strings.TrimSpace(lines[ref.Line])
			}
			out = append(out, backlink{
				NoteID:  source.NoteID,
				URI:     sourceURI,
				Path:    sourcePath,
				Title:   source.Title,
				Range:   newRange(ref.Line, ref.OpenByte, ref.Line, ref.CloseByte),
				Preview: preview,
			})
		}
	}
	if out == nil {
		out = []backlink{}
	}
	return out, nil
}

func (s *Server) references(uri string, pos position) ([]location, error) {
	if !s.inVault(uri) {
		return []location{}, nil
	}
	targetID, ok, err := s.referenceTarget(uri, pos)
	if err != nil {
		return nil, err
	}
	if !ok {
		return []location{}, nil
	}
	backlinks, err := s.backlinksTo(targetID)
	if err != nil {
		return nil, err
	}
	out := make([]location, 0, len(backlinks))
	for _, backlink := range backlinks {
		out = append(out, location{
			URI:   protocol.DocumentURI(backlink.URI),
			Range: backlink.Range,
		})
	}
	return out, nil
}

func (s *Server) referenceTarget(uri string, pos position) (int64, bool, error) {
	text, err := s.documentText(uri)
	if err != nil {
		currentID, ok := noteIDFromURI(uri)
		if ok {
			return currentID, true, nil
		}
		return 0, false, err
	}
	dict, err := s.keywordDict()
	if err != nil {
		return 0, false, err
	}
	for _, ref := range link.Refs(text) {
		if !refContainsPosition(ref, pos) {
			continue
		}
		kw, ok := dict[ref.Text]
		return kw.NoteID, ok, nil
	}
	currentID, ok := noteIDFromURI(uri)
	return currentID, ok, nil
}

func (s *Server) refreshDocumentLinks(uri string) error {
	srcID, ok := noteIDFromURI(uri)
	if !ok {
		return nil
	}
	text, err := s.documentText(uri)
	if err != nil {
		return err
	}
	dict, err := s.keywordDict()
	if err != nil {
		return err
	}
	var dstIDs []int64
	seen := map[int64]bool{}
	for _, ref := range link.Refs(text) {
		kw, ok := dict[ref.Text]
		if !ok || seen[kw.NoteID] {
			continue
		}
		seen[kw.NoteID] = true
		dstIDs = append(dstIDs, kw.NoteID)
	}
	return s.store.ReplaceLinks(srcID, dstIDs)
}

func (s *Server) definition(uri string, pos position) (*location, error) {
	if !s.inVault(uri) {
		return nil, nil
	}
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
		// A heading anchor ([[note#heading]]) navigates within the note, so same-note links
		// are still worth following; a plain self-link has nowhere to jump.
		if hasCurrentID && kw.NoteID == currentID && ref.Heading == "" {
			return nil, nil
		}
		line := 0
		if ref.Heading != "" {
			if l, found := s.headingLine(s.notePath(kw.FileKind, kw.NoteID), ref.HeadingLevel, ref.Heading); found {
				line = l
			}
		}
		return &location{
			URI:   protocol.DocumentURI(uriFromPath(s.notePath(kw.FileKind, kw.NoteID))),
			Range: newRange(line, 0, line, 0),
		}, nil
	}
	return nil, nil
}

// headingLine resolves a [[note#heading]] anchor to a 0-based line in the target note.
// It reads the target's current text (open buffer or disk) and returns the first heading whose
// level and text match. The boolean is false when the note has no such heading.
func (s *Server) headingLine(path string, level int, heading string) (int, bool) {
	text, err := s.documentText(uriFromPath(path))
	if err != nil {
		return 0, false
	}
	return link.FindHeading(text, level, heading)
}

func refContainsPosition(ref link.Ref, pos position) bool {
	return ref.Line == int(pos.Line) && int(pos.Character) >= ref.OpenByte && int(pos.Character) < ref.CloseByte
}

// completion offers note titles and aliases when the cursor sits inside an unclosed [[ on the current line.
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
		// Restricted to the title keyword (one per note) to keep the list focused; alias-keyed anchors
		// remain reachable by typing "#", which routes to headingCompletion.
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
// pre-"#" stage, where the user has typed (part of) the note name but no "#" yet. The note's own
// title heading (its first h1) is dropped as noise, matching headingCompletion. A note whose body
// cannot be read (e.g. not yet on disk) contributes nothing.
func (s *Server) headingAnchorItems(ctx openLinkContext, term, path string) []completionItem {
	body, err := s.documentText(uriFromPath(path))
	if err != nil {
		return nil
	}
	title := note.FirstH1Title(body)
	var items []completionItem
	seen := map[string]bool{}
	for _, h := range link.Headings(body) {
		if h.Level == 1 && h.Text == title {
			continue
		}
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
	// The note's title is derived from its first h1, so completing that heading just points at the
	// note itself ([[note#title]] == [[note]]). Drop it as noise; other h1 headings still appear.
	title := note.FirstH1Title(text)
	lowerPrefix := strings.ToLower(prefix)
	items := make([]completionItem, 0)
	seen := map[string]bool{}
	for _, h := range link.Headings(text) {
		if h.Level != level {
			continue
		}
		if h.Level == 1 && h.Text == title {
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

func (s *Server) codeActions(uri string, rng rangeValue) ([]codeAction, error) {
	if !s.inVault(uri) {
		return []codeAction{}, nil
	}
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
		if action, ok := s.renameRepairAction(uri, text, ref, dict); ok {
			actions = append(actions, action)
		}
		actions = append(actions, codeAction{
			Title:   fmt.Sprintf("Create note %q", title),
			Kind:    protocol.QuickFix,
			Command: createNoteLSPCommand(title, uri),
		})
	}
	mentionActions, err := s.mentionLinkActions(uri, text, rng)
	if err != nil {
		return nil, err
	}
	actions = append(actions, mentionActions...)
	if actions == nil {
		actions = []codeAction{}
	}
	return actions, nil
}

// mentionLinkActions offers, for each plain-text occurrence of a known note title or alias that
// overlaps the request range, a quick fix rewriting just that occurrence to a [[<canonical title>]] link.
func (s *Server) mentionLinkActions(uri, text string, rng rangeValue) ([]codeAction, error) {
	terms, err := s.mentionTerms(uri)
	if err != nil {
		return nil, err
	}
	var actions []codeAction
	for _, m := range mentions(text, terms) {
		mrng := newRange(m.Line, m.StartByte, m.Line, m.EndByte)
		if !rangesOverlap(rng, mrng) {
			continue
		}
		actions = append(actions, codeAction{
			Title: fmt.Sprintf("Link to [[%s]]", m.Title),
			Kind:  protocol.QuickFix,
			Edit: &workspaceEdit{
				Changes: map[documentURI][]textEdit{
					documentURI(uri): {{
						Range:   mrng,
						NewText: "[[" + m.Title + "]]",
					}},
				},
			},
		})
	}
	return actions, nil
}

// mentionTerms builds the keyword list for plain-text mention detection: every title and alias except
// the current note's own, each carrying the canonical title it should link to so alias mentions still
// rewrite to [[<title>]].
func (s *Server) mentionTerms(uri string) ([]mentionTerm, error) {
	kws, err := s.store.Keywords()
	if err != nil {
		return nil, err
	}
	titleByNote := make(map[int64]string)
	for _, kw := range kws {
		if kw.Kind == "title" {
			if _, ok := titleByNote[kw.NoteID]; !ok {
				titleByNote[kw.NoteID] = kw.Term
			}
		}
	}
	currentID, hasCurrent := noteIDFromURI(uri)
	var terms []mentionTerm
	seen := make(map[string]bool)
	for _, kw := range kws {
		if hasCurrent && kw.NoteID == currentID {
			continue
		}
		if seen[kw.Term] {
			continue
		}
		seen[kw.Term] = true
		title := kw.Term
		if t, ok := titleByNote[kw.NoteID]; ok {
			title = t
		}
		terms = append(terms, mentionTerm{Term: kw.Term, Title: title})
	}
	return terms, nil
}

// rangesOverlap reports whether two ranges share any position, treating touching endpoints as overlap
// so a zero-width cursor sitting at either edge of a mention still triggers its action.
func rangesOverlap(a, b rangeValue) bool {
	return !positionBefore(a.End, b.Start) && !positionBefore(b.End, a.Start)
}

func positionBefore(p, q position) bool {
	if p.Line != q.Line {
		return p.Line < q.Line
	}
	return p.Character < q.Character
}

func (s *Server) renameRepairAction(uri string, text string, ref link.Ref, dict map[string]store.Keyword) (codeAction, bool) {
	entry, ok, err := trackrename.LatestReachable(s.cfg.RenamesPath(), ref.Text, func(title string) bool {
		_, ok := dict[title]
		return ok
	})
	if err != nil || !ok {
		return codeAction{}, false
	}
	rng, ok := refKeyRange(text, ref)
	if !ok {
		return codeAction{}, false
	}
	edit := textEdit{
		Range:   rng,
		NewText: entry.To,
	}
	return codeAction{
		Title:       fmt.Sprintf("Rewrite link %q to renamed note %q", ref.Text, entry.To),
		Kind:        protocol.QuickFix,
		IsPreferred: true,
		Edit: &workspaceEdit{
			Changes: map[documentURI][]textEdit{
				documentURI(uri): {edit},
			},
		},
	}, true
}

func refKeyRange(text string, ref link.Ref) (rangeValue, bool) {
	lines := strings.Split(text, "\n")
	if ref.Line < 0 || ref.Line >= len(lines) || ref.StartByte > ref.EndByte || ref.EndByte > len(lines[ref.Line]) {
		return rangeValue{}, false
	}
	inner := lines[ref.Line][ref.StartByte:ref.EndByte]
	i := strings.Index(inner, ref.Text)
	if i < 0 {
		return rangeValue{}, false
	}
	start := ref.StartByte + i
	return newRange(ref.Line, start, ref.Line, start+len(ref.Text)), true
}

func rangeTouchesRef(rng rangeValue, ref link.Ref) bool {
	startLine := int(rng.Start.Line)
	endLine := int(rng.End.Line)
	startCharacter := int(rng.Start.Character)
	endCharacter := int(rng.End.Character)
	if startLine > ref.Line || endLine < ref.Line {
		return false
	}
	start := ref.OpenByte
	end := ref.CloseByte
	if startLine == endLine && startCharacter == endCharacter {
		return startLine == ref.Line && startCharacter >= start && startCharacter <= end
	}
	rangeStart := 0
	if startLine == ref.Line {
		rangeStart = startCharacter
	}
	rangeEnd := end
	if endLine == ref.Line {
		rangeEnd = endCharacter
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

func (s *Server) notePath(kind string, id int64) string {
	return s.cfg.PathForKind(kind, id)
}

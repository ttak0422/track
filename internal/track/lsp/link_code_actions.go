package lsp

import (
	"fmt"
	"strings"

	"github.com/ttak0422/track/internal/track/link"
	protocol "typefox.dev/lsp"
)

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
		actions = append(actions, codeAction{
			Title:   fmt.Sprintf("Create note %q", title),
			Kind:    protocol.QuickFix,
			Command: createNoteLSPCommand(title, uri),
		})
	}
	if action, ok := s.renameNoteAction(uri, rng); ok {
		actions = append(actions, action)
	}
	if actions == nil {
		actions = []codeAction{}
	}
	return actions, nil
}

// renameNoteAction offers renaming the note the action range targets: the note a resolved [[link]]
// under the range points to, or the current note when the range is not on a link. The work itself runs
// through textDocument/rename; this only surfaces it in the code-action menu, since rename otherwise
// requires a separate keybinding that is easy to forget.
func (s *Server) renameNoteAction(uri string, rng rangeValue) (codeAction, bool) {
	_, oldTitle, ok, err := s.renameTarget(uri, rng.Start)
	if err != nil || !ok || oldTitle == "" {
		return codeAction{}, false
	}
	return codeAction{
		Title:   fmt.Sprintf("Rename note %q", oldTitle),
		Kind:    protocol.Refactor,
		Command: renameNoteLSPCommand(uri, rng.Start, oldTitle),
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

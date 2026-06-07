package lsp

import (
	"strings"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/link"
)

// rename builds a workspace edit that renames the note targeted at pos to newName: it rewrites the
// note's first H1 and every [[oldTitle]] backlink pointing at it. It returns nil when pos does not
// target a renamable note (e.g. an unresolved link). Rename history is recorded by the normal
// save/reindex path when the rewritten H1 is parsed, so this only produces the edit.
func (s *Server) rename(uri string, pos position, newName string) (*workspaceEdit, error) {
	if !s.inVault(uri) {
		return nil, nil
	}
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return nil, nil
	}
	targetID, targetKind, oldTitle, ok, err := s.renameTarget(uri, pos)
	if err != nil {
		return nil, err
	}
	if !ok || oldTitle == "" || oldTitle == newName {
		return nil, nil
	}

	changes := map[documentURI][]textEdit{}

	targetURI := uriFromPath(s.notePath(targetKind, targetID))
	if edit, ok := s.titleEdit(targetURI); ok {
		edit.NewText = "# " + newName
		changes[documentURI(targetURI)] = append(changes[documentURI(targetURI)], edit)
	}

	backlinks, err := s.store.Backlinks(targetID)
	if err != nil {
		return nil, err
	}
	for _, src := range backlinks {
		srcURI := uriFromPath(s.notePath(src.FileKind, src.NoteID))
		text, err := s.documentText(srcURI)
		if err != nil {
			continue
		}
		for _, ref := range link.Refs(text) {
			if ref.Text != oldTitle {
				continue
			}
			rng, ok := refKeyRange(text, ref)
			if !ok {
				continue
			}
			changes[documentURI(srcURI)] = append(changes[documentURI(srcURI)], textEdit{Range: rng, NewText: newName})
		}
	}

	if len(changes) == 0 {
		return nil, nil
	}
	return &workspaceEdit{Changes: changes}, nil
}

// renameTarget resolves pos to the note a rename acts on: the note a [[link]] under the cursor points
// to, or the current note when the cursor is on its H1 title line. oldTitle is that note's current
// title, the key rewritten in backlinks.
func (s *Server) renameTarget(uri string, pos position) (id int64, kind string, oldTitle string, ok bool, err error) {
	text, err := s.documentText(uri)
	if err != nil {
		return 0, "", "", false, err
	}
	dict, err := s.keywordDict()
	if err != nil {
		return 0, "", "", false, err
	}
	for _, ref := range link.Refs(text) {
		if !refContainsPosition(ref, pos) {
			continue
		}
		kw, found := dict[ref.Text]
		if !found {
			return 0, "", "", false, nil // unresolved link: nothing to rename
		}
		return kw.NoteID, kw.FileKind, ref.Text, true, nil
	}

	// Not on a link: rename the current note when the cursor sits on its H1 title line.
	currentID, ok := noteIDFromURI(uri)
	if !ok {
		return 0, "", "", false, nil
	}
	title, line, found := firstH1(text)
	if !found || int(pos.Line) != line {
		return 0, "", "", false, nil
	}
	kind = config.KindNote
	if path, perr := pathFromURI(uri); perr == nil {
		if k, ok := s.cfg.KindFromPath(path); ok {
			kind = k
		}
	}
	return currentID, kind, title, true, nil
}

// titleEdit returns an edit spanning the target note's first H1 line (NewText left for the caller to
// set). The boolean is false when the note has no H1 or cannot be read.
func (s *Server) titleEdit(targetURI string) (textEdit, bool) {
	text, err := s.documentText(targetURI)
	if err != nil {
		return textEdit{}, false
	}
	_, line, found := firstH1(text)
	if !found {
		return textEdit{}, false
	}
	lines := strings.Split(text, "\n")
	if line < 0 || line >= len(lines) {
		return textEdit{}, false
	}
	return textEdit{Range: newRange(line, 0, line, len(lines[line]))}, true
}

// firstH1 returns the text and 0-based line of the note's first level-1 heading, skipping fenced code.
func firstH1(text string) (string, int, bool) {
	for _, h := range link.Headings(text) {
		if h.Level == 1 {
			return h.Text, h.Line, true
		}
	}
	return "", 0, false
}

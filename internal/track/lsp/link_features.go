package lsp

import (
	"strings"

	"github.com/ttak0422/track/internal/track/link"
	"github.com/ttak0422/track/internal/track/note"
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

func (s *Server) outgoingLinks(uri string) ([]outgoingLink, error) {
	if !s.inVault(uri) {
		return []outgoingLink{}, nil
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
	lines := strings.Split(text, "\n")
	var out []outgoingLink
	for _, ref := range link.Refs(text) {
		kw, ok := dict[ref.Text]
		if !ok {
			continue
		}
		if hasCurrentID && kw.NoteID == currentID && ref.Heading == "" {
			continue
		}
		preview := ""
		if ref.Line >= 0 && ref.Line < len(lines) {
			preview = strings.TrimSpace(lines[ref.Line])
		}
		targetPath := s.notePath(kw.FileKind, kw.NoteID)
		out = append(out, outgoingLink{
			NoteID:  kw.NoteID,
			URI:     uriFromPath(targetPath),
			Path:    targetPath,
			Title:   kw.Term,
			Range:   newRange(ref.Line, ref.OpenByte, ref.Line, ref.CloseByte),
			Preview: preview,
		})
	}
	if out == nil {
		out = []outgoingLink{}
	}
	return out, nil
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
		// A heading or block anchor ([[note#heading]], [[note#^id]]) navigates within the note,
		// so same-note links are still worth following; a plain self-link has nowhere to jump.
		if hasCurrentID && kw.NoteID == currentID && ref.Heading == "" && ref.BlockID == "" {
			return nil, nil
		}
		line := 0
		if ref.Heading != "" {
			if l, found := s.headingLine(s.notePath(kw.FileKind, kw.NoteID), ref.HeadingLevel, ref.Heading); found {
				line = l
			}
		}
		if ref.BlockID != "" {
			if l, found := s.blockLine(s.notePath(kw.FileKind, kw.NoteID), ref.BlockID); found {
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

// blockLine resolves a [[note#^id]] block anchor to the marked block's first line in the target note.
func (s *Server) blockLine(path, id string) (int, bool) {
	text, err := s.documentText(uriFromPath(path))
	if err != nil {
		return 0, false
	}
	from, _, found := link.FindBlock(text, id)
	return from, found
}

func refContainsPosition(ref link.Ref, pos position) bool {
	return ref.Line == int(pos.Line) && int(pos.Character) >= ref.OpenByte && int(pos.Character) < ref.CloseByte
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

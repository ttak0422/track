package lsp

import (
	"fmt"
	"strings"

	"github.com/ttak0422/track/internal/track/link"
	"github.com/ttak0422/track/internal/track/note"
	protocol "typefox.dev/lsp"
)

func (s *Server) hover(uri string, pos position) (*hover, error) {
	if !s.inVault(uri) {
		return nil, nil
	}
	text, err := s.documentText(uri)
	if err != nil {
		return nil, err
	}
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
			return &hover{
				Contents: markupContent{Kind: protocol.Markdown, Value: fmt.Sprintf("Unresolved link: `%s`", ref.Text)},
				Range:    newRange(ref.Line, ref.OpenByte, ref.Line, ref.CloseByte),
			}, nil
		}
		value, err := s.noteHoverMarkdown(kw.FileKind, kw.NoteID, ref)
		if err != nil {
			return nil, err
		}
		return &hover{
			Contents: markupContent{Kind: protocol.Markdown, Value: value},
			Range:    newRange(ref.Line, ref.OpenByte, ref.Line, ref.CloseByte),
		}, nil
	}
	return nil, nil
}

func (s *Server) noteHoverMarkdown(kind string, id int64, ref link.Ref) (string, error) {
	targetURI := uriFromPath(s.notePath(kind, id))
	raw, err := s.documentText(targetURI)
	if err != nil {
		return "", err
	}
	body, _, _ := note.SplitLegacyFootmatter(raw)
	title := note.FirstH1Title(body)
	if title == "" {
		title = fmt.Sprintf("#%d", id)
	}

	var out []string
	out = append(out, "### "+title)
	if tags := s.tagsForNote(id); len(tags) > 0 {
		for i := range tags {
			tags[i] = "#" + tags[i]
		}
		out = append(out, strings.Join(tags, " "))
	}
	if preview := hoverPreviewMarkdown(body, ref); preview != "" {
		out = append(out, "", preview)
	}
	return strings.Join(out, "\n"), nil
}

func (s *Server) tagsForNote(id int64) []string {
	refs, err := s.store.SearchRefs()
	if err != nil {
		return nil
	}
	for _, ref := range refs {
		if ref.NoteID == id {
			return append([]string(nil), ref.Tags...)
		}
	}
	return nil
}

func hoverPreviewMarkdown(markdown string, ref link.Ref) string {
	lines := strings.Split(markdown, "\n")
	start := 0
	if ref.Heading != "" {
		if line, found := link.FindHeading(markdown, ref.HeadingLevel, ref.Heading); found {
			start = line
		}
	} else if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[0]), "# ") {
		start = 1
	}

	var out []string
	chars := 0
	for i := start; i < len(lines); i++ {
		out = append(out, lines[i])
		chars += len(lines[i])
		if len(out) >= 14 || chars > 1100 {
			break
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

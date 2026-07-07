package lsp

import (
	"encoding/json"
	"fmt"

	"github.com/ttak0422/track/internal/track/link"
)

// includesCommand answers the client's "what do the ![[...]] directives on this document embed?"
// (ADR 0031). The server resolves each directive and extracts the region with the shared engine
// extractor; the client only draws (Neovim: virtual lines below the directive). Content for the
// target comes through documentText, so an open-but-unsaved target buffer embeds its live text.
const includesCommand = "track.includes"

type includesParams struct {
	URI string `json:"uri"`
}

// includeResult is one directive, resolved: where it sits (the inner-text span, the same range
// documentLink reports for the [[...]]), what it embeds, and the lines to display. A directive
// that cannot embed (unresolved key, missing heading) still appears, carrying Error, so the client
// can mark the line instead of silently showing nothing.
type includeResult struct {
	Range      rangeValue `json:"range"`
	NoteID     int64      `json:"note_id,omitempty"`
	Title      string     `json:"title,omitempty"`
	Caption    string     `json:"caption"`
	Lines      []string   `json:"lines"`
	Error      string     `json:"error,omitempty"`
	BadOptions []string   `json:"bad_options,omitempty"`
}

func (s *Server) includes(uri string) ([]includeResult, error) {
	if !s.inVault(uri) {
		return []includeResult{}, nil
	}
	text, err := s.documentText(uri)
	if err != nil {
		return nil, err
	}
	dict, err := s.keywordDict()
	if err != nil {
		return nil, err
	}
	results := []includeResult{}
	for _, inc := range link.Includes(text) {
		res := includeResult{
			Range:      newRange(inc.Line, inc.StartByte, inc.Line, inc.EndByte),
			Caption:    inc.Display,
			Lines:      []string{},
			BadOptions: inc.BadOptions,
		}
		kw, ok := dict[inc.Text]
		if !ok {
			res.Error = fmt.Sprintf("unresolved note %q", inc.Text)
			results = append(results, res)
			continue
		}
		res.NoteID = kw.NoteID
		res.Title = inc.Text
		body, err := s.documentText(uriFromPath(s.notePath(kw.FileKind, kw.NoteID)))
		if err != nil {
			res.Error = err.Error()
			results = append(results, res)
			continue
		}
		lines, ok := link.Extract(body, inc)
		if !ok {
			res.Error = fmt.Sprintf("heading not found: %s", inc.Heading)
			results = append(results, res)
			continue
		}
		res.Lines = lines
		results = append(results, res)
	}
	return results, nil
}

func includesParamsFromArgs(args []json.RawMessage) (includesParams, error) {
	if len(args) == 0 {
		return includesParams{}, fmt.Errorf("missing document uri")
	}
	var p includesParams
	if err := json.Unmarshal(args[0], &p); err != nil || p.URI == "" {
		return includesParams{}, fmt.Errorf("missing document uri")
	}
	return p, nil
}

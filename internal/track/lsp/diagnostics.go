package lsp

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ttak0422/track/internal/track/link"
	protocol "typefox.dev/lsp"
)

const (
	diagnosticSource             = "track"
	diagnosticCodeH1TitleLine    = "h1-outside-title-line"
	diagnosticCodeUnresolvedLink = "unresolved-link"
)

func (s *Server) diagnostics(uri string) ([]diagnostic, error) {
	if !s.inVault(uri) {
		return nil, nil
	}
	text, err := s.documentText(uri)
	if err != nil {
		return nil, err
	}
	diags := titleDiagnostics(text)
	links, err := s.unresolvedLinkDiagnostics(text)
	if err != nil {
		return nil, err
	}
	return append(diags, links...), nil
}

// unresolvedLinkDiagnostics warns on each [[...]] whose key matches no note title. It reuses the same
// keyword dictionary as link resolution, so the warning lines up with what documentLinks skips.
func (s *Server) unresolvedLinkDiagnostics(text string) ([]diagnostic, error) {
	dict, err := s.keywordDict()
	if err != nil {
		return nil, err
	}
	var diags []diagnostic
	for _, ref := range link.Refs(text) {
		if _, ok := dict[ref.Text]; ok {
			continue
		}
		diags = append(diags, diagnostic{
			Range:    newRange(ref.Line, ref.OpenByte, ref.Line, ref.CloseByte),
			Severity: protocol.SeverityWarning,
			Source:   diagnosticSource,
			Code:     diagnosticCodeUnresolvedLink,
			Message:  fmt.Sprintf("Unresolved link: no note titled %q", ref.Text),
		})
	}
	return diags, nil
}

func titleDiagnostics(text string) []diagnostic {
	lines := strings.Split(text, "\n")
	diagnostics := []diagnostic{}
	for _, h := range link.Headings(text) {
		if h.Level != 1 {
			continue
		}
		if h.Line == 0 {
			continue
		}
		end := 0
		if h.Line >= 0 && h.Line < len(lines) {
			end = len(lines[h.Line])
		}
		diagnostics = append(diagnostics, diagnostic{
			Range:    newRange(h.Line, 0, h.Line, end),
			Severity: protocol.SeverityWarning,
			Source:   diagnosticSource,
			Code:     diagnosticCodeH1TitleLine,
			Message:  "H1 headings are only valid on the first line, where they define the note title.",
		})
	}
	return diagnostics
}

func (s *Server) publishDiagnostics(uri string) (rpcMessage, error) {
	diagnostics, err := s.diagnostics(uri)
	if err != nil {
		return rpcMessage{}, err
	}
	if diagnostics == nil {
		diagnostics = []diagnostic{}
	}
	return newNotification("textDocument/publishDiagnostics", publishDiagnosticsParams{
		URI:         documentURI(uri),
		Diagnostics: diagnostics,
	})
}

func newNotification(method string, params any) (rpcMessage, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return rpcMessage{}, err
	}
	return rpcMessage{JSONRPC: "2.0", Method: method, Params: raw}, nil
}

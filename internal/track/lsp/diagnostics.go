package lsp

import (
	"encoding/json"
	"fmt"

	"github.com/ttak0422/track/internal/track/link"
	protocol "typefox.dev/lsp"
)

const (
	diagnosticSource               = "track"
	diagnosticCodeUnresolvedLink   = "unresolved-link"
	diagnosticCodeVaultUnavailable = "vault-unavailable"
)

func (s *Server) diagnostics(uri string) ([]diagnostic, error) {
	if !s.inVault(uri) {
		return nil, nil
	}
	text, err := s.documentText(uri)
	if err != nil {
		return nil, err
	}
	links, err := s.unresolvedLinkDiagnostics(text)
	if err != nil {
		return nil, err
	}
	return links, nil
}

// unresolvedLinkDiagnostics warns on each [[...]] whose key matches no note title. It reuses the same
// keyword dictionary as link resolution, so the warning lines up with what documentLinks skips.
// Cross-vault references are checked in their target vault; an unavailable vault is reported
// explicitly (Information) — a gap the editor can see, never a silent drop.
func (s *Server) unresolvedLinkDiagnostics(text string) ([]diagnostic, error) {
	dict, err := s.keywordDict()
	if err != nil {
		return nil, err
	}
	var diags []diagnostic
	for _, ref := range link.Refs(text) {
		if _, vault, detail, state := s.resolveQualified(ref.Text); state != notQualified {
			switch state {
			case crossMissing:
				diags = append(diags, diagnostic{
					Range:    newRange(ref.Line, ref.OpenByte, ref.Line, ref.CloseByte),
					Severity: protocol.SeverityWarning,
					Source:   diagnosticSource,
					Code:     diagnosticCodeUnresolvedLink,
					Message:  fmt.Sprintf("Unresolved link: no note titled %q in vault %q", ref.Text, vault),
				})
			case crossUnavailable:
				diags = append(diags, diagnostic{
					Range:    newRange(ref.Line, ref.OpenByte, ref.Line, ref.CloseByte),
					Severity: protocol.SeverityInformation,
					Source:   diagnosticSource,
					Code:     diagnosticCodeVaultUnavailable,
					Message:  fmt.Sprintf("Cannot check link: vault %q is unavailable (%s)", vault, detail),
				})
			}
			continue
		}
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

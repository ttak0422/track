package lsp

import (
	"encoding/json"

	protocol "typefox.dev/lsp"
)

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type position = protocol.Position
type rangeValue = protocol.Range
type location = protocol.Location
type documentURI = protocol.DocumentURI
type textEdit = protocol.TextEdit
type documentLink = protocol.DocumentLink
type completionItem = protocol.CompletionItem
type completionList = protocol.CompletionList
type command = protocol.Command
type codeAction = protocol.CodeAction
type diagnostic = protocol.Diagnostic
type workspaceEdit = protocol.WorkspaceEdit
type textDocumentIdentifier = protocol.TextDocumentIdentifier
type textDocumentPositionParams = protocol.TextDocumentPositionParams
type documentLinkParams = protocol.DocumentLinkParams
type publishDiagnosticsParams = protocol.PublishDiagnosticsParams

type backlink struct {
	NoteID  int64      `json:"note_id"`
	URI     string     `json:"uri"`
	Path    string     `json:"path"`
	Title   string     `json:"title"`
	Range   rangeValue `json:"range"`
	Preview string     `json:"preview"`
}

type codeActionParams = protocol.CodeActionParams
type executeCommandParams = protocol.ExecuteCommandParams
type didOpenParams = protocol.DidOpenTextDocumentParams
type didChangeParams = protocol.DidChangeTextDocumentParams
type didSaveParams = protocol.DidSaveTextDocumentParams
type didCloseParams = protocol.DidCloseTextDocumentParams

func newPosition(line, character int) position {
	return position{Line: uint32(line), Character: uint32(character)}
}

func newRange(startLine, startCharacter, endLine, endCharacter int) rangeValue {
	return rangeValue{
		Start: newPosition(startLine, startCharacter),
		End:   newPosition(endLine, endCharacter),
	}
}

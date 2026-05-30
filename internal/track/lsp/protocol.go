package lsp

import "encoding/json"

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

type position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type rangeValue struct {
	Start position `json:"start"`
	End   position `json:"end"`
}

type location struct {
	URI   string     `json:"uri"`
	Range rangeValue `json:"range"`
}

type textEdit struct {
	Range   rangeValue `json:"range"`
	NewText string     `json:"newText"`
}

type documentLink struct {
	Range   rangeValue `json:"range"`
	Target  string     `json:"target"`
	Tooltip string     `json:"tooltip,omitempty"`
}

// completionKindReference is LSP CompletionItemKind.Reference, used for note-link candidates.
const completionKindReference = 18

type completionItem struct {
	Label      string    `json:"label"`
	Kind       int       `json:"kind,omitempty"`
	Detail     string    `json:"detail,omitempty"`
	InsertText string    `json:"insertText,omitempty"`
	FilterText string    `json:"filterText,omitempty"`
	SortText   string    `json:"sortText,omitempty"`
	TextEdit   *textEdit `json:"textEdit,omitempty"`
	Command    *command  `json:"command,omitempty"`
}

type completionList struct {
	IsIncomplete bool             `json:"isIncomplete"`
	Items        []completionItem `json:"items"`
}

type command struct {
	Title     string `json:"title"`
	Command   string `json:"command"`
	Arguments []any  `json:"arguments,omitempty"`
}

type codeAction struct {
	Title   string   `json:"title"`
	Kind    string   `json:"kind,omitempty"`
	Command *command `json:"command,omitempty"`
}

type textDocumentIdentifier struct {
	URI string `json:"uri"`
}

type textDocumentPositionParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Position     position               `json:"position"`
}

type documentLinkParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
}

type codeActionParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Range        rangeValue             `json:"range"`
}

type executeCommandParams struct {
	Command   string            `json:"command"`
	Arguments []json.RawMessage `json:"arguments,omitempty"`
}

type didOpenParams struct {
	TextDocument struct {
		URI  string `json:"uri"`
		Text string `json:"text"`
	} `json:"textDocument"`
}

type didChangeParams struct {
	TextDocument   textDocumentIdentifier `json:"textDocument"`
	ContentChanges []struct {
		Text string `json:"text"`
	} `json:"contentChanges"`
}

type didSaveParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Text         *string                `json:"text,omitempty"`
}

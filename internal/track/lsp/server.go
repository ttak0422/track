package lsp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/link"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

type Server struct {
	cfg   *config.Config
	store *store.Store
	docs  map[string]string
}

const createNoteCommand = "track.createNote"

func Run(in io.Reader, out io.Writer) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	s, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer s.Close()

	return NewServer(cfg, s).Serve(in, out)
}

func NewServer(cfg *config.Config, s *store.Store) *Server {
	return &Server{cfg: cfg, store: s, docs: map[string]string{}}
}

func (s *Server) Serve(in io.Reader, out io.Writer) error {
	r := bufio.NewReader(in)
	for {
		msg, err := readMessage(r)
		if err != nil {
			if isDisconnect(err) {
				return nil
			}
			return err
		}
		if msg.Method == "exit" {
			return nil
		}
		if msg.ID == nil {
			s.handleNotification(msg)
			continue
		}
		resp := s.handleRequest(msg)
		if err := writeMessage(out, resp); err != nil {
			if isDisconnect(err) {
				return nil
			}
			return err
		}
	}
}

// isDisconnect reports whether err is just the editor closing the connection (e.g. during shutdown),
// which ends the session normally rather than failing the server. Neovim can close stdin/stdout mid
// message, so a partial read (ErrUnexpectedEOF) or a broken pipe (EPIPE) is expected, not an error.
func isDisconnect(err error) bool {
	return errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, os.ErrClosed) ||
		errors.Is(err, syscall.EPIPE)
}

func (s *Server) handleNotification(msg rpcMessage) {
	switch msg.Method {
	case "initialized":
	case "textDocument/didOpen":
		var p didOpenParams
		if json.Unmarshal(msg.Params, &p) == nil {
			s.docs[p.TextDocument.URI] = p.TextDocument.Text
		}
	case "textDocument/didChange":
		var p didChangeParams
		if json.Unmarshal(msg.Params, &p) == nil && len(p.ContentChanges) > 0 {
			s.docs[p.TextDocument.URI] = p.ContentChanges[len(p.ContentChanges)-1].Text
		}
	case "textDocument/didSave":
		var p didSaveParams
		if json.Unmarshal(msg.Params, &p) == nil {
			if p.Text != nil {
				s.docs[p.TextDocument.URI] = *p.Text
			}
			if path, err := pathFromURI(p.TextDocument.URI); err == nil {
				_ = index.New(s.cfg, s.store).One(path)
			}
		}
	}
}

func (s *Server) handleRequest(msg rpcMessage) rpcMessage {
	resp := rpcMessage{JSONRPC: "2.0", ID: msg.ID}
	switch msg.Method {
	case "initialize":
		resp.Result = map[string]any{
			"serverInfo": map[string]any{"name": "track-lsp", "version": "0.1.0"},
			"capabilities": map[string]any{
				"positionEncoding":        "utf-8",
				"textDocumentSync":        1,
				"definitionProvider":      true,
				"documentLinkProvider":    map[string]any{"resolveProvider": false},
				"completionProvider":      map[string]any{"triggerCharacters": []string{"["}},
				"codeActionProvider":      true,
				"executeCommandProvider":  map[string]any{"commands": []string{createNoteCommand}},
				"workspaceSymbolProvider": false,
			},
		}
	case "shutdown":
		resp.Result = nil
	case "textDocument/documentLink":
		var p documentLinkParams
		if err := json.Unmarshal(msg.Params, &p); err != nil {
			resp.Error = &rpcError{Code: -32602, Message: err.Error()}
			return resp
		}
		links, err := s.documentLinks(p.TextDocument.URI)
		if err != nil {
			resp.Error = &rpcError{Code: -32000, Message: err.Error()}
			return resp
		}
		resp.Result = links
	case "textDocument/definition":
		var p textDocumentPositionParams
		if err := json.Unmarshal(msg.Params, &p); err != nil {
			resp.Error = &rpcError{Code: -32602, Message: err.Error()}
			return resp
		}
		loc, err := s.definition(p.TextDocument.URI, p.Position)
		if err != nil {
			resp.Error = &rpcError{Code: -32000, Message: err.Error()}
			return resp
		}
		resp.Result = loc
	case "textDocument/completion":
		var p textDocumentPositionParams
		if err := json.Unmarshal(msg.Params, &p); err != nil {
			resp.Error = &rpcError{Code: -32602, Message: err.Error()}
			return resp
		}
		items, err := s.completion(p.TextDocument.URI, p.Position)
		if err != nil {
			resp.Error = &rpcError{Code: -32000, Message: err.Error()}
			return resp
		}
		resp.Result = completionList{IsIncomplete: true, Items: items}
	case "textDocument/codeAction":
		var p codeActionParams
		if err := json.Unmarshal(msg.Params, &p); err != nil {
			resp.Error = &rpcError{Code: -32602, Message: err.Error()}
			return resp
		}
		actions, err := s.codeActions(p.TextDocument.URI, p.Range)
		if err != nil {
			resp.Error = &rpcError{Code: -32000, Message: err.Error()}
			return resp
		}
		resp.Result = actions
	case "workspace/executeCommand":
		var p executeCommandParams
		if err := json.Unmarshal(msg.Params, &p); err != nil {
			resp.Error = &rpcError{Code: -32602, Message: err.Error()}
			return resp
		}
		result, err := s.executeCommand(p)
		if err != nil {
			resp.Error = &rpcError{Code: -32000, Message: err.Error()}
			return resp
		}
		resp.Result = result
	default:
		resp.Error = &rpcError{Code: -32601, Message: "method not found"}
	}
	return resp
}

func (s *Server) documentLinks(uri string) ([]documentLink, error) {
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
		if hasCurrentID && kw.NoteID == currentID {
			continue
		}
		links = append(links, documentLink{
			Range: rangeValue{
				Start: position{Line: ref.Line, Character: ref.StartByte},
				End:   position{Line: ref.Line, Character: ref.EndByte},
			},
			Target:  uriFromPath(kw.Path),
			Tooltip: ref.Text,
		})
	}
	if links == nil {
		links = []documentLink{}
	}
	return links, nil
}

func (s *Server) definition(uri string, pos position) (*location, error) {
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
		if ref.Line != pos.Line || pos.Character < ref.StartByte || pos.Character >= ref.EndByte {
			continue
		}
		kw, ok := dict[ref.Text]
		if !ok {
			return nil, nil
		}
		if hasCurrentID && kw.NoteID == currentID {
			return nil, nil
		}
		return &location{
			URI: uriFromPath(kw.Path),
			Range: rangeValue{
				Start: position{Line: 0, Character: 0},
				End:   position{Line: 0, Character: 0},
			},
		}, nil
	}
	return nil, nil
}

// completion offers note titles and aliases when the cursor sits inside an unclosed [[ on the current line.
// Existing candidates come from the same dictionary that resolves links. If the typed target has no
// matching keyword, an extra item lets the client create a note from that input.
func (s *Server) completion(uri string, pos position) ([]completionItem, error) {
	text, err := s.documentText(uri)
	if err != nil {
		return nil, err
	}
	ctx, ok := openLinkCompletionContext(text, pos)
	if !ok {
		return []completionItem{}, nil
	}
	currentID, hasCurrentID := noteIDFromURI(uri)
	kws, err := s.store.Keywords()
	if err != nil {
		return nil, err
	}
	items := make([]completionItem, 0, len(kws))
	hasPrefixMatch := false
	for _, kw := range kws {
		if ctx.Target != "" && strings.HasPrefix(strings.ToLower(kw.Term), strings.ToLower(ctx.Target)) {
			hasPrefixMatch = true
		}
		if hasCurrentID && kw.NoteID == currentID {
			continue
		}
		items = append(items, completionItem{
			Label:      kw.Term,
			Kind:       completionKindReference,
			Detail:     kw.Kind,
			InsertText: kw.Term,
			TextEdit:   completionTextEdit(ctx, kw.Term),
		})
	}
	if ctx.Target != "" && !hasPrefixMatch {
		items = append(items, createNoteCompletionItem(uri, ctx))
	}
	return items, nil
}

// insideOpenLink reports whether pos sits after a "[[" with no closing "]]" before it on the same line.
func insideOpenLink(text string, pos position) bool {
	lines := strings.Split(text, "\n")
	if pos.Line < 0 || pos.Line >= len(lines) {
		return false
	}
	line := lines[pos.Line]
	col := pos.Character
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
	Target       string
}

func openLinkCompletionContext(text string, pos position) (openLinkContext, bool) {
	lines := strings.Split(text, "\n")
	if pos.Line < 0 || pos.Line >= len(lines) {
		return openLinkContext{}, false
	}
	line := lines[pos.Line]
	col := pos.Character
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
	return openLinkContext{
		Line:         pos.Line,
		ReplaceStart: open + 2,
		ReplaceEnd:   col,
		Target:       strings.TrimSpace(typed),
	}, true
}

func completionTextEdit(ctx openLinkContext, text string) *textEdit {
	return &textEdit{
		Range: rangeValue{
			Start: position{Line: ctx.Line, Character: ctx.ReplaceStart},
			End:   position{Line: ctx.Line, Character: ctx.ReplaceEnd},
		},
		NewText: text,
	}
}

func createNoteCompletionItem(uri string, ctx openLinkContext) completionItem {
	return completionItem{
		Label:      ctx.Target,
		Kind:       completionKindReference,
		Detail:     "create note",
		InsertText: ctx.Target,
		FilterText: ctx.Target,
		SortText:   ctx.Target,
		TextEdit:   completionTextEdit(ctx, ctx.Target),
		Command:    createNoteLSPCommand(ctx.Target, uri),
	}
}

func (s *Server) codeActions(uri string, rng rangeValue) ([]codeAction, error) {
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
			Kind:    "quickfix",
			Command: createNoteLSPCommand(title, uri),
		})
	}
	if actions == nil {
		actions = []codeAction{}
	}
	return actions, nil
}

func createNoteLSPCommand(title, uri string) *command {
	return &command{
		Title:   fmt.Sprintf("Create note %q", title),
		Command: createNoteCommand,
		Arguments: []any{
			map[string]any{
				"title": title,
				"uri":   uri,
			},
		},
	}
}

func rangeTouchesRef(rng rangeValue, ref link.Ref) bool {
	if rng.Start.Line > ref.Line || rng.End.Line < ref.Line {
		return false
	}
	start := ref.OpenByte
	end := ref.CloseByte
	if rng.Start.Line == rng.End.Line && rng.Start.Character == rng.End.Character {
		return rng.Start.Line == ref.Line && rng.Start.Character >= start && rng.Start.Character <= end
	}
	rangeStart := 0
	if rng.Start.Line == ref.Line {
		rangeStart = rng.Start.Character
	}
	rangeEnd := end
	if rng.End.Line == ref.Line {
		rangeEnd = rng.End.Character
	}
	return rangeStart <= end && rangeEnd >= start
}

func (s *Server) executeCommand(p executeCommandParams) (map[string]any, error) {
	if p.Command != createNoteCommand {
		return nil, fmt.Errorf("unsupported command %q", p.Command)
	}
	title, err := createNoteTitleFromArgs(p.Arguments)
	if err != nil {
		return nil, err
	}
	return s.createNote(title)
}

func createNoteTitleFromArgs(args []json.RawMessage) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("missing note title")
	}
	var obj struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal(args[0], &obj); err == nil && obj.Title != "" {
		return obj.Title, nil
	}
	var title string
	if err := json.Unmarshal(args[0], &title); err == nil && title != "" {
		return title, nil
	}
	return "", fmt.Errorf("missing note title")
}

func (s *Server) createNote(title string) (map[string]any, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, fmt.Errorf("note title is required")
	}
	if _, ok, err := s.resolveKeyword(title); err != nil {
		return nil, err
	} else if ok {
		return nil, fmt.Errorf("note already exists for %q", title)
	}

	noteID := time.Now().Unix()
	for {
		if _, err := os.Stat(s.cfg.NotePath(noteID)); os.IsNotExist(err) {
			break
		} else if err != nil {
			return nil, err
		}
		noteID++
	}
	path := s.cfg.NotePath(noteID)
	if err := os.MkdirAll(s.cfg.VaultDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte("# "+title+"\n"), 0o644); err != nil {
		return nil, err
	}
	if err := note.WriteMetadata(
		s.cfg.MetadataPath(noteID),
		note.Metadata{Title: title, Created: time.Now().Format(s.cfg.DateFormat)},
	); err != nil {
		return nil, err
	}
	if err := index.New(s.cfg, s.store).One(path); err != nil {
		return nil, err
	}
	return map[string]any{
		"id":    noteID,
		"path":  path,
		"uri":   uriFromPath(path),
		"title": title,
	}, nil
}

func (s *Server) resolveKeyword(term string) (store.Keyword, bool, error) {
	dict, err := s.keywordDict()
	if err != nil {
		return store.Keyword{}, false, err
	}
	kw, ok := dict[term]
	return kw, ok, nil
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

func (s *Server) documentText(uri string) (string, error) {
	if text, ok := s.docs[uri]; ok {
		return text, nil
	}
	path, err := pathFromURI(uri)
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func readMessage(r *bufio.Reader) (rpcMessage, error) {
	length := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return rpcMessage{}, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return rpcMessage{}, fmt.Errorf("invalid header %q", line)
		}
		if strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return rpcMessage{}, err
			}
			length = n
		}
	}
	if length < 0 {
		return rpcMessage{}, fmt.Errorf("missing Content-Length")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return rpcMessage{}, err
	}
	var msg rpcMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		return rpcMessage{}, err
	}
	return msg, nil
}

func writeMessage(w io.Writer, msg rpcMessage) error {
	if msg.JSONRPC == "" {
		msg.JSONRPC = "2.0"
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "Content-Length: %d\r\n\r\n%s", len(body), body)
	return err
}

func pathFromURI(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Scheme != "file" {
		return "", fmt.Errorf("unsupported uri scheme %q", u.Scheme)
	}
	path, err := url.PathUnescape(u.Path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(path), nil
}

func uriFromPath(path string) string {
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	return u.String()
}

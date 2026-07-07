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
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/store"
	protocol "typefox.dev/lsp"
)

type Server struct {
	cfg   *config.Config
	store *store.Store
	docs  map[string]string
}

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
			notifications := s.handleNotification(msg)
			for _, notification := range notifications {
				if err := writeMessage(out, notification); err != nil {
					if isDisconnect(err) {
						return nil
					}
					return err
				}
			}
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

func (s *Server) handleNotification(msg rpcMessage) []rpcMessage {
	switch msg.Method {
	case "initialized":
	case "textDocument/didOpen":
		var p didOpenParams
		if json.Unmarshal(msg.Params, &p) == nil {
			uri := string(p.TextDocument.URI)
			s.docs[uri] = p.TextDocument.Text
			if notification, err := s.publishDiagnostics(uri); err == nil {
				return []rpcMessage{notification}
			}
		}
	case "textDocument/didChange":
		var p didChangeParams
		if json.Unmarshal(msg.Params, &p) == nil && len(p.ContentChanges) > 0 {
			uri := string(p.TextDocument.URI)
			s.docs[uri] = p.ContentChanges[len(p.ContentChanges)-1].Text
			if notification, err := s.publishDiagnostics(uri); err == nil {
				return []rpcMessage{notification}
			}
		}
	case "textDocument/didSave":
		var p didSaveParams
		if json.Unmarshal(msg.Params, &p) == nil {
			uri := string(p.TextDocument.URI)
			if p.Text != nil {
				s.docs[uri] = *p.Text
			}
			if s.inVault(uri) {
				if path, err := pathFromURI(uri); err == nil {
					_ = index.New(s.cfg, s.store).One(path)
				}
			}
			if notification, err := s.publishDiagnostics(uri); err == nil {
				return []rpcMessage{notification}
			}
		}
	case "textDocument/didClose":
		var p didCloseParams
		if json.Unmarshal(msg.Params, &p) == nil {
			uri := string(p.TextDocument.URI)
			delete(s.docs, uri)
			if s.inVault(uri) {
				notification, err := newNotification("textDocument/publishDiagnostics", publishDiagnosticsParams{
					URI:         documentURI(uri),
					Diagnostics: []diagnostic{},
				})
				if err == nil {
					return []rpcMessage{notification}
				}
			}
		}
	}
	return nil
}

func (s *Server) handleRequest(msg rpcMessage) rpcMessage {
	resp := rpcMessage{JSONRPC: "2.0", ID: msg.ID}
	// Index-backed features resolve [[links]] against the keyword index; refresh it from disk first so
	// notes created or changed outside this process (CLI, web, another editor, cloud sync) are visible.
	switch msg.Method {
	case "textDocument/documentLink", "track/backlinks", "track/outgoingLinks",
		"textDocument/definition", "textDocument/hover", "textDocument/references",
		"textDocument/completion", "textDocument/codeAction", "textDocument/rename":
		s.refreshIndex()
	}
	switch msg.Method {
	case "initialize":
		encoding := protocol.UTF8
		resp.Result = protocol.InitializeResult{
			ServerInfo: &protocol.ServerInfo{Name: "track-lsp", Version: "0.1.0"},
			Capabilities: protocol.ServerCapabilities{
				PositionEncoding:       &encoding,
				TextDocumentSync:       protocol.Full,
				DefinitionProvider:     &protocol.Or_ServerCapabilities_definitionProvider{Value: true},
				ReferencesProvider:     &protocol.Or_ServerCapabilities_referencesProvider{Value: true},
				HoverProvider:          &protocol.Or_ServerCapabilities_hoverProvider{Value: true},
				DocumentLinkProvider:   &protocol.DocumentLinkOptions{ResolveProvider: false},
				CompletionProvider:     &protocol.CompletionOptions{TriggerCharacters: []string{"[", "#", ":", " ", "<", "?", "&", "="}},
				CodeActionProvider:     true,
				RenameProvider:         &protocol.Or_ServerCapabilities_renameProvider{Value: true},
				ExecuteCommandProvider: &protocol.ExecuteCommandOptions{Commands: []string{createNoteCommand, includesCommand}},
				WorkspaceSymbolProvider: &protocol.Or_ServerCapabilities_workspaceSymbolProvider{
					Value: false,
				},
			},
		}
	case "shutdown":
		resp.Result = json.RawMessage("null")
	case "textDocument/documentLink":
		var p documentLinkParams
		if err := json.Unmarshal(msg.Params, &p); err != nil {
			resp.Error = &rpcError{Code: -32602, Message: err.Error()}
			return resp
		}
		links, err := s.documentLinks(string(p.TextDocument.URI))
		if err != nil {
			resp.Error = &rpcError{Code: -32000, Message: err.Error()}
			return resp
		}
		resp.Result = links
	case "track/backlinks":
		var p documentLinkParams
		if err := json.Unmarshal(msg.Params, &p); err != nil {
			resp.Error = &rpcError{Code: -32602, Message: err.Error()}
			return resp
		}
		backlinks, err := s.backlinks(string(p.TextDocument.URI))
		if err != nil {
			resp.Error = &rpcError{Code: -32000, Message: err.Error()}
			return resp
		}
		resp.Result = backlinks
	case "track/outgoingLinks":
		var p documentLinkParams
		if err := json.Unmarshal(msg.Params, &p); err != nil {
			resp.Error = &rpcError{Code: -32602, Message: err.Error()}
			return resp
		}
		links, err := s.outgoingLinks(string(p.TextDocument.URI))
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
		loc, err := s.definition(string(p.TextDocument.URI), p.Position)
		if err != nil {
			resp.Error = &rpcError{Code: -32000, Message: err.Error()}
			return resp
		}
		resp.Result = loc
	case "textDocument/hover":
		var p hoverParams
		if err := json.Unmarshal(msg.Params, &p); err != nil {
			resp.Error = &rpcError{Code: -32602, Message: err.Error()}
			return resp
		}
		hov, err := s.hover(string(p.TextDocument.URI), p.Position)
		if err != nil {
			resp.Error = &rpcError{Code: -32000, Message: err.Error()}
			return resp
		}
		resp.Result = hov
	case "textDocument/references":
		var p textDocumentPositionParams
		if err := json.Unmarshal(msg.Params, &p); err != nil {
			resp.Error = &rpcError{Code: -32602, Message: err.Error()}
			return resp
		}
		refs, err := s.references(string(p.TextDocument.URI), p.Position)
		if err != nil {
			resp.Error = &rpcError{Code: -32000, Message: err.Error()}
			return resp
		}
		resp.Result = refs
	case "textDocument/completion":
		var p textDocumentPositionParams
		if err := json.Unmarshal(msg.Params, &p); err != nil {
			resp.Error = &rpcError{Code: -32602, Message: err.Error()}
			return resp
		}
		items, err := s.completion(string(p.TextDocument.URI), p.Position)
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
		actions, err := s.codeActions(string(p.TextDocument.URI), p.Range)
		if err != nil {
			resp.Error = &rpcError{Code: -32000, Message: err.Error()}
			return resp
		}
		resp.Result = actions
	case "textDocument/rename":
		var p renameParams
		if err := json.Unmarshal(msg.Params, &p); err != nil {
			resp.Error = &rpcError{Code: -32602, Message: err.Error()}
			return resp
		}
		edit, err := s.rename(string(p.TextDocument.URI), p.Position, p.NewName)
		if err != nil {
			resp.Error = &rpcError{Code: -32000, Message: err.Error()}
			return resp
		}
		resp.Result = edit
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

// refreshIndex brings the keyword index back in sync with disk before serving an index-backed query.
// A long-lived LSP process never observes notes created, renamed, or removed by another process (the
// CLI, the web server, a second editor) or by a cloud-sync write, because those raise no LSP event.
// RefreshIfStale is cheap on the common unchanged path (it only stats directory entries), so calling
// it here lets a [[link]] to such a note resolve immediately instead of failing until an unrelated
// didSave happens to reindex. Mirrors the self-heal the web server and CLI already do before a query.
// A refresh error is non-fatal: serving from the current index degrades the same way it did before.
func (s *Server) refreshIndex() {
	_, _ = index.New(s.cfg, s.store).RefreshIfStale()
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

// inVault reports whether uri names a track note: a file with a supported extension that lives
// inside the configured vault. Markdown is a common format, so editors routinely attach this server
// to files that are not track notes (this repo's own README, docs under .track, scratch files
// elsewhere). Feature handlers gate on this so those buffers get no links, completion, or actions.
func (s *Server) inVault(uri string) bool {
	path, err := pathFromURI(uri)
	if err != nil {
		return false
	}
	if !slices.Contains(s.cfg.Extensions, filepath.Ext(path)) {
		return false
	}
	vaultDir := canonicalPath(s.cfg.VaultDir)
	path = canonicalPath(path)
	rel, err := filepath.Rel(vaultDir, path)
	if err != nil {
		return false
	}
	// rel escaping the vault starts with ".." (or is exactly ".."); anything else is inside it.
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	rel = filepath.Clean(rel)
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) != 2 || (parts[0] != config.KindNote && parts[0] != config.KindJournal) {
		return false
	}
	// Exclude track-owned hidden directories such as .track, matching the indexer's scan rules.
	for _, part := range strings.Split(filepath.Dir(rel), string(filepath.Separator)) {
		if part != "." && strings.HasPrefix(part, ".") {
			return false
		}
	}
	_, ok := noteIDFromURI(uri)
	return ok
}

func canonicalPath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = filepath.Clean(abs)
	} else {
		path = filepath.Clean(path)
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	var suffix []string
	for dir := path; ; dir = filepath.Dir(dir) {
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		suffix = append(suffix, filepath.Base(dir))
		if realDir, err := filepath.EvalSymlinks(parent); err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				realDir = filepath.Join(realDir, suffix[i])
			}
			return realDir
		}
	}
	return path
}

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
	"github.com/ttak0422/track/internal/track/editorview"
	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
	"github.com/ttak0422/track/internal/track/vaultref"
	protocol "typefox.dev/lsp"
)

type Server struct {
	cfg             *config.Config
	store           *store.Store
	docs            map[string]string
	versions        map[string]int32
	docPaths        map[string]string
	pendingWarnings []rpcMessage
	warned          map[string]bool
	// xv resolves cross-vault [[vault:title]] references, lazily opening and caching the other
	// registered vaults' configs and stores for the server's lifetime.
	xv *vaultref.Resolver
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
	return &Server{cfg: cfg, store: s, docs: map[string]string{}, versions: map[string]int32{}, docPaths: map[string]string{}, warned: map[string]bool{}, xv: vaultref.New(cfg)}
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
			notifications = append(notifications, s.pendingWarnings...)
			s.pendingWarnings = nil
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
		for _, warning := range s.pendingWarnings {
			if err := writeMessage(out, warning); err != nil {
				return err
			}
		}
		s.pendingWarnings = nil
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

// The protocol library normalizes DocumentURI percent escapes during unmarshal. Preserve the
// actual client spelling at the document-ownership boundary before using its typed params.
func rawTextDocumentURI(params json.RawMessage, fallback string) string {
	var raw struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
	}
	if json.Unmarshal(params, &raw) == nil && raw.TextDocument.URI != "" {
		return raw.TextDocument.URI
	}
	return fallback
}

func (s *Server) handleNotification(msg rpcMessage) []rpcMessage {
	switch msg.Method {
	case "initialized":
	case "textDocument/didOpen":
		var p didOpenParams
		if json.Unmarshal(msg.Params, &p) == nil {
			uri := rawTextDocumentURI(msg.Params, string(p.TextDocument.URI))
			s.docs[uri] = p.TextDocument.Text
			s.versions[uri] = p.TextDocument.Version
			if _, err := s.documentOwner(uri); err != nil {
				return s.identityWarning(err)
			}
			if notification, err := s.publishDiagnostics(uri); err == nil {
				return []rpcMessage{notification}
			}
		}
	case "textDocument/didChange":
		var p didChangeParams
		if json.Unmarshal(msg.Params, &p) == nil && len(p.ContentChanges) > 0 {
			uri := rawTextDocumentURI(msg.Params, string(p.TextDocument.URI))
			if previous := s.versions[uri]; previous > 0 && p.TextDocument.Version <= previous {
				return nil
			}
			s.versions[uri] = p.TextDocument.Version
			s.docs[uri] = p.ContentChanges[len(p.ContentChanges)-1].Text
			if _, err := s.documentOwner(uri); err != nil {
				return s.identityWarning(err)
			}
			if notification, err := s.publishDiagnostics(uri); err == nil {
				return []rpcMessage{notification}
			}
		}
	case "textDocument/didSave":
		var p didSaveParams
		if json.Unmarshal(msg.Params, &p) == nil {
			uri := rawTextDocumentURI(msg.Params, string(p.TextDocument.URI))
			if _, err := s.documentOwner(uri); err != nil {
				return s.identityWarning(err)
			}
			if p.Text != nil {
				s.docs[uri] = *p.Text
			}
			if s.inVault(uri) {
				if path, err := s.canonicalDocumentPath(uri); err == nil {
					if _, err := editorview.Resolve(s.cfg, path); err != nil {
						return s.identityWarning(err)
					}
					if err := index.New(s.cfg, s.store).One(path); err != nil {
						return s.identityWarning(err)
					}
				} else {
					return s.identityWarning(err)
				}
			}
			if notification, err := s.publishDiagnostics(uri); err == nil {
				return []rpcMessage{notification}
			}
		}
	case "textDocument/didClose":
		var p didCloseParams
		if json.Unmarshal(msg.Params, &p) == nil {
			uri := rawTextDocumentURI(msg.Params, string(p.TextDocument.URI))
			delete(s.docs, uri)
			delete(s.versions, uri)
			delete(s.docPaths, uri)
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
	owner, err := s.documentOwner(uri)
	if err != nil {
		return "", err
	}
	if owner != "" {
		return s.docs[owner], nil
	}
	path, err := s.canonicalDocumentPath(uri)
	if err != nil {
		// Cross-vault navigation is read-only here; resolve its approved alias against its own manifest.
		path, err = pathFromURI(uri)
		if err != nil {
			return "", err
		}
		if editorview.IsViewPath(path) {
			_, entry, e := editorview.Discover(path)
			if e != nil {
				return "", e
			}
			path = entry.Path
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// documentOwner joins aliases and canonical URIs by stable identity while retaining the URI the
// client actually opened. Two independent buffers are a conflict, never last-notification-wins.
func (s *Server) documentOwner(uri string) (string, error) {
	path, err := s.canonicalDocumentPath(uri)
	if err != nil {
		if path, e := pathFromURI(uri); e == nil && editorview.IsViewPath(path) {
			// An outgoing cross-vault location has its own approved manifest, not this server's.
			// It is read-only here; that vault's own LSP client owns any open editing buffer.
			if cfg, _, discoveryErr := editorview.Discover(path); discoveryErr == nil && cfg.VaultDir != s.cfg.VaultDir {
				return "", nil
			}
			return "", err
		}
		if _, pinned := s.docPaths[uri]; pinned {
			return "", err
		}
		if _, open := s.docs[uri]; open {
			return uri, nil
		}
		return "", nil
	}
	owner := ""
	for candidate := range s.docs {
		candidatePath, e := s.canonicalDocumentPath(candidate)
		if pinned, ok := s.docPaths[candidate]; ok {
			if pinned == path && (e != nil || candidatePath != pinned) {
				return "", fmt.Errorf("open buffer's canonical identity is unavailable or changed: %s", candidate)
			}
		} else if e == nil {
			s.docPaths[candidate] = candidatePath
		}
		if e != nil || candidatePath != path {
			continue
		}
		if owner != "" && owner != candidate {
			return "", fmt.Errorf("note is open under multiple URIs; close one buffer before editing: %s and %s", owner, candidate)
		}
		owner = candidate
	}
	return owner, nil
}

func (s *Server) identityWarning(err error) []rpcMessage {
	notification, e := newNotification("window/showMessage", map[string]any{"type": 1, "message": "track: " + err.Error()})
	if e != nil {
		return nil
	}
	return []rpcMessage{notification}
}

// preferredPath does not canonicalize an open buffer's displayed URI.
// uriForPath preserves the exact client's spelling (including percent-escape casing), not only
// the decoded pathname, when addressing an open document.
func (s *Server) uriForPath(path string) string {
	uri := uriFromPath(path)
	if owner, err := s.documentOwner(uri); err == nil && owner != "" {
		return owner
	}
	return uri
}

func (s *Server) preferredPath(path string) string {
	if owner, err := s.documentOwner(uriFromPath(path)); err == nil && owner != "" {
		if opened, err := pathFromURI(owner); err == nil {
			return opened
		}
	}
	return path
}

func (s *Server) editorPath(cfg *config.Config, id int64, kind, title string) string {
	path := cfg.PathForKind(kind, id)
	if cfg.VaultDir == s.cfg.VaultDir {
		if owner, err := s.documentOwner(uriFromPath(path)); err == nil && owner != "" {
			if opened, err := pathFromURI(owner); err == nil {
				return opened
			}
		}
	}
	if editorview.Enabled() {
		if entry, err := editorview.Ensure(cfg, kind, id, title); err == nil {
			return entry.EditorPath
		} else {
			message := "readable editor path unavailable; using canonical path: " + err.Error()
			if !s.warned[message] {
				s.warned[message] = true
				s.pendingWarnings = append(s.pendingWarnings, s.identityWarning(errors.New(message))...)
			}
		}
	}
	return path
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
	if u.Scheme != "file" || (u.Host != "" && u.Host != "localhost") {
		return "", fmt.Errorf("unsupported uri scheme %q", u.Scheme)
	}
	if !filepath.IsAbs(u.Path) {
		return "", fmt.Errorf("file URI must use an absolute path")
	}
	return filepath.Clean(u.Path), nil
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
	_, err := s.canonicalDocumentPath(uri)
	return err == nil
}

func (s *Server) canonicalDocumentPath(uri string) (string, error) {
	path, err := pathFromURI(uri)
	if err != nil {
		return "", err
	}
	if !slices.Contains(s.cfg.Extensions, filepath.Ext(path)) {
		return "", fmt.Errorf("unsupported note extension")
	}
	if editorview.IsViewPath(path) {
		entry, err := editorview.Resolve(s.cfg, path)
		return entry.Path, err
	}
	// Ordinary canonical paths may name an unsaved/new buffer. Only the vault prefix may be a
	// symlink; nonnumeric title aliases are accepted exclusively through the view manifest above.
	if _, err := note.IDFromPath(path); err != nil {
		return "", err
	}
	kind := filepath.Base(filepath.Dir(path))
	if kind != config.KindNote && kind != config.KindJournal {
		return "", fmt.Errorf("not a canonical vault note")
	}
	vault := canonicalPath(s.cfg.VaultDir)
	if canonicalPath(filepath.Dir(filepath.Dir(path))) != vault {
		return "", fmt.Errorf("not a canonical vault note")
	}
	if info, err := os.Lstat(filepath.Dir(path)); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("canonical note kind directory must not be a symlink")
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("canonical note must be a regular file, not a symlink")
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	path = filepath.Join(vault, kind, filepath.Base(path))
	return path, nil
}

func (s *Server) noteIDFromURI(uri string) (int64, bool) {
	path, err := s.canonicalDocumentPath(uri)
	if err != nil {
		return 0, false
	}
	id, err := note.IDFromPath(path)
	return id, err == nil
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

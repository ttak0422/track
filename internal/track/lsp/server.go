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

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/store"
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

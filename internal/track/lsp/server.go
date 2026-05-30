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

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/match"
	"github.com/ttak0422/track/internal/track/note"
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
			if errors.Is(err, io.EOF) {
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
			return err
		}
	}
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
	m, refs, err := s.matcher()
	if err != nil {
		return nil, err
	}
	var links []documentLink
	for _, occ := range m.Occurrences(text) {
		if hasCurrentID && occ.Term.NoteID == currentID {
			continue
		}
		ref, ok := refs[occ.Term.NoteID]
		if !ok {
			continue
		}
		links = append(links, documentLink{
			Range: rangeValue{
				Start: position{Line: occ.Line, Character: occ.StartByte},
				End:   position{Line: occ.Line, Character: occ.EndByte},
			},
			Target:  uriFromPath(ref.Path),
			Tooltip: occ.Term.Text,
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
	m, refs, err := s.matcher()
	if err != nil {
		return nil, err
	}
	for _, occ := range m.Occurrences(text) {
		if hasCurrentID && occ.Term.NoteID == currentID {
			continue
		}
		if occ.Line != pos.Line || pos.Character < occ.StartByte || pos.Character >= occ.EndByte {
			continue
		}
		ref, ok := refs[occ.Term.NoteID]
		if !ok {
			return nil, nil
		}
		return &location{
			URI: uriFromPath(ref.Path),
			Range: rangeValue{
				Start: position{Line: 0, Character: 0},
				End:   position{Line: 0, Character: 0},
			},
		}, nil
	}
	return nil, nil
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

func (s *Server) matcher() (*match.Matcher, map[int64]store.Keyword, error) {
	kws, err := s.store.Keywords()
	if err != nil {
		return nil, nil, err
	}
	terms := make([]match.Term, 0, len(kws))
	refs := make(map[int64]store.Keyword, len(kws))
	for _, kw := range kws {
		terms = append(terms, match.Term{Text: kw.Term, NoteID: kw.NoteID})
		if _, ok := refs[kw.NoteID]; !ok {
			refs[kw.NoteID] = kw
		}
	}
	return match.New(terms), refs, nil
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

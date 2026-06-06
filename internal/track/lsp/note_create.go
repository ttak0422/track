package lsp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/note"
)

const createNoteCommand = "track.createNote"

func createNoteLSPCommand(title, uri string) *command {
	arg, _ := json.Marshal(map[string]any{
		"title": title,
		"uri":   uri,
	})
	return &command{
		Title:     fmt.Sprintf("Create note %q", title),
		Command:   createNoteCommand,
		Arguments: []json.RawMessage{arg},
	}
}

func (s *Server) executeCommand(p executeCommandParams) (map[string]any, error) {
	if p.Command != createNoteCommand {
		return nil, fmt.Errorf("unsupported command %q", p.Command)
	}
	params, err := createNoteParamsFromArgs(p.Arguments)
	if err != nil {
		return nil, err
	}
	result, err := s.createNote(params.Title)
	if err != nil {
		return nil, err
	}
	if params.URI != "" {
		_ = s.refreshDocumentLinks(params.URI)
	}
	return result, nil
}

type createNoteParams struct {
	Title string `json:"title"`
	URI   string `json:"uri,omitempty"`
}

func createNoteParamsFromArgs(args []json.RawMessage) (createNoteParams, error) {
	if len(args) == 0 {
		return createNoteParams{}, fmt.Errorf("missing note title")
	}
	var obj createNoteParams
	if err := json.Unmarshal(args[0], &obj); err == nil && obj.Title != "" {
		return obj, nil
	}
	var title string
	if err := json.Unmarshal(args[0], &title); err == nil && title != "" {
		return createNoteParams{Title: title}, nil
	}
	return createNoteParams{}, fmt.Errorf("missing note title")
}

func (s *Server) createNote(title string) (map[string]any, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, fmt.Errorf("note title is required")
	}
	dict, err := s.keywordDict()
	if err != nil {
		return nil, err
	}
	if _, ok := dict[title]; ok {
		return nil, fmt.Errorf("note already exists for %q", title)
	}

	noteID, err := note.NewID(s.cfg, time.Now())
	if err != nil {
		return nil, err
	}
	path := s.cfg.NotePath(noteID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
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

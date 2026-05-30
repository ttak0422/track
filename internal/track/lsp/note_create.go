package lsp

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/note"
)

const createNoteCommand = "track.createNote"

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
	dict, err := s.keywordDict()
	if err != nil {
		return nil, err
	}
	if _, ok := dict[title]; ok {
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

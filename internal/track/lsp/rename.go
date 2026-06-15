package lsp

import (
	"os"
	"strings"

	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/link"
	"github.com/ttak0422/track/internal/track/note"
	trackrename "github.com/ttak0422/track/internal/track/rename"
)

// rename renames the metadata title of the note targeted at pos and returns a workspace edit for
// backlinks that spell the old title. It returns nil when pos does not target a renamable note
// (e.g. an unresolved link).
func (s *Server) rename(uri string, pos position, newName string) (*workspaceEdit, error) {
	if !s.inVault(uri) {
		return nil, nil
	}
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return nil, nil
	}
	targetID, oldTitle, ok, err := s.renameTarget(uri, pos)
	if err != nil {
		return nil, err
	}
	if !ok || oldTitle == "" || oldTitle == newName {
		return nil, nil
	}
	if ref, found, err := s.store.ResolveTerm(newName); err != nil {
		return nil, err
	} else if found && ref.NoteID != targetID {
		return nil, nil
	}

	changes := map[documentURI][]textEdit{}

	backlinks, err := s.store.Backlinks(targetID)
	if err != nil {
		return nil, err
	}
	for _, src := range backlinks {
		srcURI := uriFromPath(s.notePath(src.FileKind, src.NoteID))
		text, err := s.documentText(srcURI)
		if err != nil {
			continue
		}
		if _, open := s.docs[srcURI]; open {
			// The editor owns this buffer (it may hold unsaved edits), so hand it a workspace edit
			// rather than writing the file ourselves. didSave will reindex it once the user saves.
			for _, ref := range link.Refs(text) {
				if ref.Text != oldTitle {
					continue
				}
				rng, ok := refKeyRange(text, ref)
				if !ok {
					continue
				}
				changes[documentURI(srcURI)] = append(changes[documentURI(srcURI)], textEdit{Range: rng, NewText: newName})
			}
			continue
		}
		// Closed file: rewrite it on disk so the reindex below reads the new link text. A workspace
		// edit only reaches open buffers, so without this the backlink would silently drop until the
		// file is next opened and saved.
		rewritten, n := link.ReplaceRefKey(text, oldTitle, newName)
		if n == 0 {
			continue
		}
		path, err := pathFromURI(srcURI)
		if err != nil {
			continue
		}
		if err := os.WriteFile(path, []byte(rewritten), 0o644); err != nil {
			return nil, err
		}
	}

	meta, _, err := note.ReadMetadata(s.cfg.MetadataPath(targetID))
	if err != nil {
		return nil, err
	}
	meta.Title = newName
	if err := note.WriteMetadata(s.cfg.MetadataPath(targetID), meta); err != nil {
		return nil, err
	}
	if err := trackrename.Append(s.cfg.RenamesPath(), trackrename.Entry{From: oldTitle, To: newName, NoteID: targetID}); err != nil {
		return nil, err
	}
	if _, err := index.New(s.cfg, s.store).Full(); err != nil {
		return nil, err
	}

	if len(changes) == 0 {
		return nil, nil
	}
	return &workspaceEdit{Changes: changes}, nil
}

// renameTarget resolves pos to the note a rename acts on: the note a [[link]] under the cursor points
// to, or the current note when the cursor is not on a link. oldTitle is that note's current title,
// the key rewritten in backlinks.
func (s *Server) renameTarget(uri string, pos position) (id int64, oldTitle string, ok bool, err error) {
	text, err := s.documentText(uri)
	if err != nil {
		return 0, "", false, err
	}
	dict, err := s.keywordDict()
	if err != nil {
		return 0, "", false, err
	}
	for _, ref := range link.Refs(text) {
		if !refContainsPosition(ref, pos) {
			continue
		}
		kw, found := dict[ref.Text]
		if !found {
			return 0, "", false, nil // unresolved link: nothing to rename
		}
		return kw.NoteID, ref.Text, true, nil
	}

	// Not on a link: rename the current note's metadata title.
	currentID, ok := noteIDFromURI(uri)
	if !ok {
		return 0, "", false, nil
	}
	meta, found, err := note.ReadMetadata(s.cfg.MetadataPath(currentID))
	if err != nil {
		return 0, "", false, err
	}
	if !found || meta.Title == "" {
		return 0, "", false, nil
	}
	return currentID, meta.Title, true, nil
}

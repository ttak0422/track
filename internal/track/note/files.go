package note

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ttak0422/track/internal/track/config"
)

// WriteVerify writes exact bytes and reads them back before returning. Mutation workflows use it
// before touching a source file so a failed destination write duplicates data rather than losing it.
func WriteVerify(path string, content []byte) error {
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	back, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("verify %s: %w", path, err)
	}
	if !bytes.Equal(back, content) {
		return fmt.Errorf("write verification failed for %s", path)
	}
	return nil
}

// TrashMove records the paths used by MoveToTrash so a caller can restore a source when a later
// operation fails.
type TrashMove struct {
	NotePath        string
	MetadataPath    string
	TrashedNote     string
	TrashedMetadata string
}

// MoveToTrash moves a note body and sidecar into the vault trash under one timestamp. If the
// sidecar leg fails, the body is restored to its original path whenever possible.
func MoveToTrash(cfg *config.Config, notePath string, noteID int64) (TrashMove, error) {
	move := TrashMove{NotePath: notePath, MetadataPath: cfg.MetadataPath(noteID)}
	if err := os.MkdirAll(cfg.TrashDir(), 0o755); err != nil {
		return move, fmt.Errorf("create trash dir: %w", err)
	}
	stamp := time.Now().UnixMilli()
	move.TrashedNote = filepath.Join(cfg.TrashDir(), fmt.Sprintf("%d-%s", stamp, filepath.Base(notePath)))
	if err := os.Rename(notePath, move.TrashedNote); err != nil {
		return move, fmt.Errorf("move note: %w", err)
	}
	if _, err := os.Stat(move.MetadataPath); err == nil {
		move.TrashedMetadata = filepath.Join(cfg.TrashDir(), fmt.Sprintf("%d-%s", stamp, filepath.Base(move.MetadataPath)))
		if err := os.Rename(move.MetadataPath, move.TrashedMetadata); err != nil {
			if restoreErr := os.Rename(move.TrashedNote, notePath); restoreErr != nil {
				return move, fmt.Errorf("move sidecar: %w; body remains at %s (restore failed: %v)", err, move.TrashedNote, restoreErr)
			}
			return move, fmt.Errorf("move sidecar: %w", err)
		}
	} else if !os.IsNotExist(err) {
		if restoreErr := os.Rename(move.TrashedNote, notePath); restoreErr != nil {
			return move, fmt.Errorf("inspect sidecar: %w; body remains at %s (restore failed: %v)", err, move.TrashedNote, restoreErr)
		}
		return move, fmt.Errorf("inspect sidecar: %w", err)
	}
	return move, nil
}

// Restore puts a successful trash move back at its original paths. It is used when a later step in
// a larger operation fails; failure is reported without discarding either trashed file.
func (m TrashMove) Restore() error {
	if m.TrashedMetadata != "" {
		if err := os.Rename(m.TrashedMetadata, m.MetadataPath); err != nil {
			return fmt.Errorf("restore metadata from %s: %w", m.TrashedMetadata, err)
		}
	}
	if err := os.Rename(m.TrashedNote, m.NotePath); err != nil {
		return fmt.Errorf("restore note from %s: %w", m.TrashedNote, err)
	}
	return nil
}

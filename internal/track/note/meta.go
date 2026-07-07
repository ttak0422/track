package note

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/track/config"
)

// MetaEdit is one page-metadata change: a nil field is left untouched, a pointer to "" clears the
// field. It is the single write path for description/image, shared by the CLI meta command and the
// web editor, so validation lives here once.
type MetaEdit struct {
	Description *string
	Image       *string
}

// imageExtensions lists the cover-image formats OGP consumers actually render; anything else (an
// SVG, a PDF) errors at set time instead of publishing a broken og:image.
var imageExtensions = []string{".png", ".jpg", ".jpeg", ".webp", ".gif"}

// ApplyMetaEdit validates an edit against the vault and writes it into the note's sidecar, returning
// the resulting metadata. The image must be an existing vault asset addressed as "assets/<file>" —
// the same reference form note bodies use — with no absolute path or traversal, mirroring the
// data-source rules elsewhere. The description is stored trimmed; renderers flatten it further.
func ApplyMetaEdit(cfg *config.Config, noteID int64, edit MetaEdit) (Metadata, error) {
	metaPath := cfg.MetadataPath(noteID)
	meta, found, err := ReadMetadata(metaPath)
	if err != nil {
		return Metadata{}, fmt.Errorf("read metadata: %w", err)
	}
	if !found {
		meta = Metadata{Created: time.Now().Format(cfg.DateFormat)}
	}
	if edit.Description != nil {
		meta.Description = strings.TrimSpace(*edit.Description)
	}
	if edit.Image != nil {
		img := strings.TrimSpace(*edit.Image)
		if img != "" {
			if err := validateImageRef(cfg, img); err != nil {
				return Metadata{}, err
			}
		}
		meta.Image = img
	}
	if err := WriteMetadata(metaPath, meta); err != nil {
		return Metadata{}, fmt.Errorf("write metadata: %w", err)
	}
	return meta, nil
}

// validateImageRef checks a cover-image reference: assets/-relative, no escape from the assets
// directory, a renderable raster format, and the file actually present.
func validateImageRef(cfg *config.Config, ref string) error {
	if filepath.IsAbs(ref) || strings.Contains(ref, "..") {
		return fmt.Errorf("image %q must be a plain assets/<file> reference", ref)
	}
	rel, ok := strings.CutPrefix(filepath.ToSlash(ref), config.AssetsDirName+"/")
	if !ok || rel == "" {
		return fmt.Errorf("image %q must live under %s/ (import it with `track asset import`)", ref, config.AssetsDirName)
	}
	if !slices.Contains(imageExtensions, strings.ToLower(filepath.Ext(rel))) {
		return fmt.Errorf("image %q must be one of %s (OGP consumers do not render other formats)", ref, strings.Join(imageExtensions, " "))
	}
	if _, err := os.Stat(filepath.Join(cfg.AssetsDir(), filepath.FromSlash(rel))); err != nil {
		return fmt.Errorf("image %q not found in the vault assets: %w", ref, err)
	}
	return nil
}

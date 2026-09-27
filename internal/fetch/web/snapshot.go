package web

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const SnapshotExtractionMethod = "readability-v1"

// SnapshotManifest describes the two files produced by the opt-in web snapshot mode. Paths are
// fixed names inside a newly-created unique child directory, never caller-controlled paths.
type SnapshotManifest struct {
	SchemaVersion    int          `json:"schema_version"`
	SourceURL        string       `json:"source_url"`
	FinalURL         string       `json:"final_url"`
	RetrievedAt      string       `json:"retrieved_at"`
	OriginalPath     string       `json:"original_path"`
	TextPath         string       `json:"text_path"`
	OriginalSHA256   string       `json:"original_sha256"`
	TextSHA256       string       `json:"text_sha256"`
	ExtractionMethod string       `json:"extraction_method"`
	Published        DateMetadata `json:"published"`
	Modified         DateMetadata `json:"modified"`
}

// SaveSnapshot creates a unique child beneath dir and saves the response body and extracted Markdown.
// If any file operation fails, files created by this call and its child directory are removed when
// possible; a cleanup failure is returned with the remaining partial path spelled out.
func SaveSnapshot(dir, sourceURL, finalURL string, retrievedAt time.Time, original []byte, page Page) (SnapshotManifest, error) {
	if dir == "" {
		return SnapshotManifest{}, fmt.Errorf("snapshot directory is required")
	}
	if sourceURL == "" || finalURL == "" {
		return SnapshotManifest{}, fmt.Errorf("snapshot source and final URLs are required")
	}
	if retrievedAt.IsZero() {
		return SnapshotManifest{}, fmt.Errorf("snapshot retrieval time is required")
	}

	manifest := SnapshotManifest{
		SchemaVersion:    1,
		SourceURL:        sourceURL,
		FinalURL:         finalURL,
		RetrievedAt:      retrievedAt.UTC().Format(time.RFC3339Nano),
		OriginalSHA256:   sha256Hex(original),
		TextSHA256:       sha256Hex([]byte(page.Markdown)),
		ExtractionMethod: SnapshotExtractionMethod,
		Published:        withDatePrecision(page.Published),
		Modified:         withDatePrecision(page.Modified),
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return SnapshotManifest{}, fmt.Errorf("resolve snapshot directory: %w", err)
	}
	if filepath.Clean(root) == string(filepath.Separator) {
		return SnapshotManifest{}, fmt.Errorf("snapshot directory must not be the filesystem root")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return SnapshotManifest{}, fmt.Errorf("create snapshot directory %q: %w", dir, err)
	}
	snapshotDir, err := os.MkdirTemp(root, "snapshot-")
	if err != nil {
		return SnapshotManifest{}, fmt.Errorf("create unique snapshot directory: %w", err)
	}
	manifest.OriginalPath = filepath.Join(snapshotDir, "original.html")
	manifest.TextPath = filepath.Join(snapshotDir, "text.md")

	if err := writeSnapshotFiles(snapshotDir, original, []byte(page.Markdown), manifest); err != nil {
		return SnapshotManifest{}, err
	}
	return manifest, nil
}

func withDatePrecision(metadata DateMetadata) DateMetadata {
	if metadata.Precision == "" {
		metadata.Precision = "absent"
	}
	return metadata
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func writeSnapshotFiles(dir string, original, text []byte, manifest SnapshotManifest) error {
	created := make([]string, 0, 2)
	for _, file := range []struct {
		name string
		data []byte
	}{
		{name: filepath.Base(manifest.OriginalPath), data: original},
		{name: filepath.Base(manifest.TextPath), data: text},
	} {
		path := filepath.Join(dir, file.name)
		made, writeErr := writeSnapshotFile(path, file.data)
		if made {
			created = append(created, path)
		}
		if writeErr != nil {
			return cleanupPartialSnapshot(dir, created, fmt.Errorf("write %s: %w", file.name, writeErr))
		}
	}
	return nil
}

func writeSnapshotFile(path string, data []byte) (bool, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return false, err
	}
	n, writeErr := f.Write(data)
	if writeErr == nil && n != len(data) {
		writeErr = fmt.Errorf("short write: wrote %d of %d bytes", n, len(data))
	}
	if writeErr == nil {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		return true, errors.Join(writeErr, closeErr)
	}
	return true, nil
}

func cleanupPartialSnapshot(dir string, files []string, cause error) error {
	var cleanupErr error
	for _, file := range files {
		if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
			cleanupErr = errors.Join(cleanupErr, err)
		}
	}
	if err := os.Remove(dir); err != nil && !os.IsNotExist(err) {
		cleanupErr = errors.Join(cleanupErr, err)
	}
	if cleanupErr != nil {
		return fmt.Errorf("%w; partial snapshot remains at %q (cleanup failed: %v)", cause, dir, cleanupErr)
	}
	return fmt.Errorf("%w; partial snapshot at %q was removed", cause, dir)
}

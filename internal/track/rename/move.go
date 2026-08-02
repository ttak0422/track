package rename

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/link"
	"github.com/ttak0422/track/internal/track/note"
	tracksite "github.com/ttak0422/track/internal/track/site"
	"github.com/ttak0422/track/internal/track/store"
)

// LinkPolicy selects what a move does with local links that cannot resolve in the destination.
type LinkPolicy string

const (
	RefuseBrokenLinks  LinkPolicy = "refuse"
	UnlinkBrokenLinks  LinkPolicy = "unlink"
	QualifyBrokenLinks LinkPolicy = "qualify"
)

// MoveResult reports the durable destination and source backlink changes.
type MoveResult struct {
	NoteID           int64
	Title            string
	DestinationPath  string
	BacklinksUpdated int
}

// Move transfers a note between vaults while preserving its id and sidecar bytes. All destination
// conflicts are checked from authoritative vault files before any destination or cache write. Once
// writing begins, every failure before the final source reindex leaves or restores the source,
// preferring a duplicate to lost text.
func Move(srcCfg, dstCfg *config.Config, srcStore *store.Store, srcPath, dstName string, policy LinkPolicy) (MoveResult, error) {
	var res MoveResult
	if policy != RefuseBrokenLinks && policy != UnlinkBrokenLinks && policy != QualifyBrokenLinks {
		return res, fmt.Errorf("unknown broken-link policy %q", policy)
	}
	if _, err := index.New(srcCfg, srcStore).Full(); err != nil {
		return res, fmt.Errorf("reindex source before move: %w", err)
	}

	noteID, err := note.IDFromPath(srcPath)
	if err != nil {
		return res, fmt.Errorf("invalid note path: %w", err)
	}
	meta, found, err := note.ReadMetadata(srcCfg.MetadataPath(noteID))
	if err != nil {
		return res, fmt.Errorf("read metadata: %w", err)
	}
	if !found {
		return res, fmt.Errorf("no metadata for note %d", noteID)
	}
	body, err := os.ReadFile(srcPath)
	if err != nil {
		return res, fmt.Errorf("read note: %w", err)
	}
	metaBytes, err := os.ReadFile(srcCfg.MetadataPath(noteID))
	if err != nil {
		return res, fmt.Errorf("read metadata: %w", err)
	}
	res.NoteID, res.Title = noteID, meta.Title

	kind, ok := srcCfg.KindFromPath(srcPath)
	if !ok {
		return res, fmt.Errorf("path is not a vault note: %s", srcPath)
	}
	if kind == config.KindJournal && dstCfg.JournalOff {
		return res, fmt.Errorf("destination vault %q has journal notes disabled", dstName)
	}
	res.DestinationPath = dstCfg.PathForKind(kind, noteID)
	dstTitles, dstIDs, err := vaultNotes(dstCfg)
	if err != nil {
		return res, fmt.Errorf("scan destination titles: %w", err)
	}
	if dstIDs[noteID] || fileExists(dstCfg.TemplatePath(noteID)) || fileExists(dstCfg.MetadataPath(noteID)) {
		return res, fmt.Errorf("destination vault %q already has note id %d", dstName, noteID)
	}
	if existingID, exists := dstTitles[meta.Title]; exists {
		return res, fmt.Errorf("destination vault %q already has title %q on note %d", dstName, meta.Title, existingID)
	}

	var unresolved []string
	unresolvedSet := map[string]bool{}
	for _, ref := range link.Refs(string(body)) {
		if _, _, qualified := link.SplitVaultRef(ref.Text, func(name string) bool {
			_, exists := srcCfg.Vaults[name]
			return exists
		}); qualified {
			continue
		}
		if ref.Text == meta.Title {
			continue
		}
		if _, exists := dstTitles[ref.Text]; !exists && !unresolvedSet[ref.Text] {
			unresolvedSet[ref.Text] = true
			unresolved = append(unresolved, ref.Text)
		}
	}
	if len(unresolved) > 0 && policy == RefuseBrokenLinks {
		return res, fmt.Errorf("move would leave unresolved local links in %q: %s (use --unlink or --qualify)", dstName, strings.Join(unresolved, ", "))
	}
	if len(unresolved) > 0 && policy == QualifyBrokenLinks && srcCfg.VaultName == "" {
		return res, fmt.Errorf("--qualify requires the source vault to have a registered name")
	}
	if policy == QualifyBrokenLinks {
		for _, key := range unresolved {
			rewritten, _ := link.ReplaceRefKey(string(body), key, srcCfg.VaultName+":"+key)
			body = []byte(rewritten)
		}
	}
	if policy == UnlinkBrokenLinks {
		rewritten, _ := link.UnlinkRefKeys(string(body), unresolvedSet)
		body = []byte(rewritten)
	}

	assetData := map[string][]byte{}
	assetRefs := tracksite.CollectAssets(string(body))
	if meta.Image != "" {
		if err := note.ValidateImageRef(srcCfg, meta.Image); err != nil {
			return res, err
		}
		assetRefs = append(assetRefs, strings.TrimPrefix(filepath.ToSlash(meta.Image), config.AssetsDirName+"/"))
	}
	for _, rel := range assetRefs {
		if _, duplicate := assetData[rel]; duplicate {
			continue
		}
		srcAsset := filepath.Join(srcCfg.AssetsDir(), filepath.FromSlash(rel))
		data, err := os.ReadFile(srcAsset)
		if err != nil {
			return res, fmt.Errorf("read referenced asset %q: %w", config.AssetsDirName+"/"+rel, err)
		}
		assetData[rel] = data
		dstAsset := filepath.Join(dstCfg.AssetsDir(), filepath.FromSlash(rel))
		if existing, err := os.ReadFile(dstAsset); err == nil {
			if !bytes.Equal(existing, data) {
				return res, fmt.Errorf("destination asset %q already exists with different content", config.AssetsDirName+"/"+rel)
			}
		} else if !os.IsNotExist(err) {
			return res, fmt.Errorf("inspect destination asset %q: %w", config.AssetsDirName+"/"+rel, err)
		}
	}

	if err := os.MkdirAll(filepath.Dir(res.DestinationPath), 0o755); err != nil {
		return res, fmt.Errorf("create destination note dir: %w", err)
	}
	if err := note.WriteVerify(res.DestinationPath, body); err != nil {
		return res, fmt.Errorf("write destination note: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dstCfg.MetadataPath(noteID)), 0o755); err != nil {
		return res, fmt.Errorf("create destination metadata dir: %w", err)
	}
	if err := note.WriteVerify(dstCfg.MetadataPath(noteID), metaBytes); err != nil {
		return res, fmt.Errorf("write destination metadata: %w", err)
	}
	for rel, data := range assetData {
		dstAsset := filepath.Join(dstCfg.AssetsDir(), filepath.FromSlash(rel))
		if fileExists(dstAsset) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dstAsset), 0o755); err != nil {
			return res, fmt.Errorf("create destination asset dir: %w", err)
		}
		if err := note.WriteVerify(dstAsset, data); err != nil {
			return res, fmt.Errorf("write destination asset %q: %w", config.AssetsDirName+"/"+rel, err)
		}
	}

	backlinks, err := srcStore.Backlinks(noteID)
	if err != nil {
		return res, fmt.Errorf("backlinks: %w", err)
	}
	for _, src := range backlinks {
		backlinkPath := srcCfg.PathForKind(src.FileKind, src.NoteID)
		raw, err := os.ReadFile(backlinkPath)
		if err != nil {
			return res, fmt.Errorf("read backlink %d: %w", src.NoteID, err)
		}
		rewritten, n := link.ReplaceRefKey(string(raw), meta.Title, dstName+":"+meta.Title)
		if n == 0 {
			continue
		}
		if err := note.WriteVerify(backlinkPath, []byte(rewritten)); err != nil {
			return res, fmt.Errorf("write backlink %d: %w", src.NoteID, err)
		}
		res.BacklinksUpdated += n
	}

	dstStore, err := store.Open(dstCfg.DBPath)
	if err != nil {
		return res, fmt.Errorf("open destination index: %w", err)
	}
	defer dstStore.Close()
	if _, err := index.New(dstCfg, dstStore).Full(); err != nil {
		return res, fmt.Errorf("reindex destination: %w", err)
	}

	trashed, err := note.MoveToTrash(srcCfg, srcPath, noteID)
	if err != nil {
		return res, fmt.Errorf("move source to trash: %w", err)
	}
	if _, err := index.New(srcCfg, srcStore).Full(); err != nil {
		if restoreErr := trashed.Restore(); restoreErr != nil {
			return res, fmt.Errorf("reindex source: %w; source restore failed: %v", err, restoreErr)
		}
		_, _ = index.New(srcCfg, srcStore).Full()
		return res, fmt.Errorf("reindex source: %w (source restored)", err)
	}
	return res, nil
}

func vaultNotes(cfg *config.Config) (map[string]int64, map[int64]bool, error) {
	titles := map[string]int64{}
	ids := map[int64]bool{}
	for _, dir := range []string{cfg.NoteDir(), cfg.JournalDir()} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() || !slices.Contains(cfg.Extensions, filepath.Ext(entry.Name())) {
				continue
			}
			id, err := note.IDFromPath(entry.Name())
			if err != nil {
				continue
			}
			ids[id] = true
			meta, found, err := note.ReadMetadata(cfg.MetadataPath(id))
			if err != nil {
				return nil, nil, err
			}
			if found && meta.Title != "" {
				titles[meta.Title] = id
			}
		}
	}
	return titles, ids, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

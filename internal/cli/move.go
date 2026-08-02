package cli

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/link"
	"github.com/ttak0422/track/internal/track/note"
	tracksite "github.com/ttak0422/track/internal/track/site"
	"github.com/ttak0422/track/internal/track/store"
)

// cmdMv moves one note between registered vaults. The destination is written and read back before
// the source is touched; every later failure deliberately leaves the source in place, preferring a
// duplicate to lost text.
func cmdMv(args []string) int {
	fs := flag.NewFlagSet("mv", flag.ContinueOnError)
	id := fs.Int64("id", 0, "note id")
	title := fs.String("title", "", "note title (alternative to --id)")
	path := fs.String("path", "", "note path (alternative to --id)")
	to := fs.String("to", "", "registered destination vault name")
	unlink := fs.Bool("unlink", false, "turn destination-local links that would break into plain text")
	qualify := fs.Bool("qualify", false, "qualify destination-local links back to the source vault")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}
	dstName := strings.TrimSpace(*to)
	if dstName == "" {
		return fail("--to is required")
	}
	if *unlink && *qualify {
		return fail("--unlink and --qualify are mutually exclusive")
	}

	srcCfg, srcStore, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer srcStore.Close()
	dstRoot, ok := srcCfg.Vaults[dstName]
	if !ok {
		return fail("unknown destination vault %q", dstName)
	}
	if srcCfg.VaultName == dstName {
		return fail("destination vault %q is already active", dstName)
	}
	if st, err := os.Stat(dstRoot); err != nil || !st.IsDir() {
		return fail("destination vault %q is unavailable: %v", dstName, err)
	}

	srcPath, err := resolveNotePath(srcCfg, srcStore, *id, strings.TrimSpace(*title), strings.TrimSpace(*path))
	if err != nil {
		return fail("%v", err)
	}
	noteID, err := note.IDFromPath(srcPath)
	if err != nil {
		return fail("invalid note path: %v", err)
	}
	meta, found, err := note.ReadMetadata(srcCfg.MetadataPath(noteID))
	if err != nil {
		return fail("read metadata: %v", err)
	}
	if !found {
		return fail("no metadata for note %d", noteID)
	}
	bodyBytes, err := os.ReadFile(srcPath)
	if err != nil {
		return fail("read note: %v", err)
	}
	metaBytes, err := os.ReadFile(srcCfg.MetadataPath(noteID))
	if err != nil {
		return fail("read metadata: %v", err)
	}

	dstCfg, err := config.LoadAt(dstRoot)
	if err != nil {
		return fail("load destination vault: %v", err)
	}
	dstStore, err := store.Open(dstCfg.DBPath)
	if err != nil {
		return fail("open destination index: %v", err)
	}
	defer dstStore.Close()
	if _, err := index.New(dstCfg, dstStore).Full(); err != nil {
		return fail("reindex destination: %v", err)
	}

	kind, ok := srcCfg.KindFromPath(srcPath)
	if !ok {
		return fail("path is not a vault note: %s", srcPath)
	}
	if kind == config.KindJournal && dstCfg.JournalOff {
		return fail("destination vault %q has journal notes disabled", dstName)
	}
	dstPath := dstCfg.PathForKind(kind, noteID)
	if fileExists(dstCfg.NotePath(noteID)) || fileExists(dstCfg.JournalPath(fmt.Sprint(noteID))) || fileExists(dstCfg.MetadataPath(noteID)) {
		return fail("destination vault %q already has note id %d", dstName, noteID)
	}
	if ref, exists, err := dstStore.ResolveTerm(meta.Title); err != nil {
		return fail("resolve destination title: %v", err)
	} else if exists {
		return fail("destination vault %q already has title %q on note %d", dstName, meta.Title, ref.NoteID)
	}

	// A local link stays valid only when the destination already owns that title. Self-links are
	// valid too: the moved note itself will provide its title after the copy.
	var unresolved []string
	seen := map[string]bool{}
	for _, ref := range link.Refs(string(bodyBytes)) {
		if _, _, qualified := link.SplitVaultRef(ref.Text, func(name string) bool {
			_, exists := srcCfg.Vaults[name]
			return exists
		}); qualified {
			continue
		}
		if ref.Text == meta.Title {
			continue
		}
		if _, exists, err := dstStore.ResolveTerm(ref.Text); err != nil {
			return fail("resolve destination link %q: %v", ref.Text, err)
		} else if !exists && !seen[ref.Text] {
			seen[ref.Text] = true
			unresolved = append(unresolved, ref.Text)
		}
	}
	if len(unresolved) > 0 && !*unlink && !*qualify {
		return fail("move would leave unresolved local links in %q: %s (use --unlink or --qualify)", dstName, strings.Join(unresolved, ", "))
	}
	if len(unresolved) > 0 && *qualify && srcCfg.VaultName == "" {
		return fail("--qualify requires the source vault to have a registered name")
	}
	if *qualify {
		for _, key := range unresolved {
			body, _ := link.ReplaceRefKey(string(bodyBytes), key, srcCfg.VaultName+":"+key)
			bodyBytes = []byte(body)
		}
	}
	if *unlink {
		body, _ := link.UnlinkRefKeys(string(bodyBytes), seen)
		bodyBytes = []byte(body)
	}

	assetData := map[string][]byte{}
	assetRefs := tracksite.CollectAssets(string(bodyBytes))
	if strings.HasPrefix(meta.Image, config.AssetsDirName+"/") {
		assetRefs = append(assetRefs, strings.TrimPrefix(meta.Image, config.AssetsDirName+"/"))
	}
	for _, rel := range assetRefs {
		if _, duplicate := assetData[rel]; duplicate {
			continue
		}
		srcAsset := filepath.Join(srcCfg.AssetsDir(), filepath.FromSlash(rel))
		data, err := os.ReadFile(srcAsset)
		if err != nil {
			return fail("read referenced asset %q: %v", config.AssetsDirName+"/"+rel, err)
		}
		assetData[rel] = data
		dstAsset := filepath.Join(dstCfg.AssetsDir(), filepath.FromSlash(rel))
		if existing, err := os.ReadFile(dstAsset); err == nil {
			if !bytes.Equal(existing, data) {
				return fail("destination asset %q already exists with different content", config.AssetsDirName+"/"+rel)
			}
		} else if !os.IsNotExist(err) {
			return fail("inspect destination asset %q: %v", config.AssetsDirName+"/"+rel, err)
		}
	}

	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return fail("create destination note dir: %v", err)
	}
	if err := writeVerify(dstPath, string(bodyBytes)); err != nil {
		return fail("write destination note: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(dstCfg.MetadataPath(noteID)), 0o755); err != nil {
		return fail("create destination metadata dir: %v", err)
	}
	if err := writeVerify(dstCfg.MetadataPath(noteID), string(metaBytes)); err != nil {
		return fail("write destination metadata: %v", err)
	}
	for rel, data := range assetData {
		dstAsset := filepath.Join(dstCfg.AssetsDir(), filepath.FromSlash(rel))
		if fileExists(dstAsset) { // identical content was accepted during preflight
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dstAsset), 0o755); err != nil {
			return fail("create destination asset dir: %v", err)
		}
		if err := writeVerify(dstAsset, string(data)); err != nil {
			return fail("write destination asset %q: %v", config.AssetsDirName+"/"+rel, err)
		}
	}

	backlinks, err := srcStore.Backlinks(noteID)
	if err != nil {
		return fail("backlinks: %v", err)
	}
	backlinksUpdated := 0
	for _, src := range backlinks {
		backlinkPath := srcCfg.PathForKind(src.FileKind, src.NoteID)
		raw, err := os.ReadFile(backlinkPath)
		if err != nil {
			return fail("read backlink %d: %v", src.NoteID, err)
		}
		rewritten, n := link.ReplaceRefKey(string(raw), meta.Title, dstName+":"+meta.Title)
		if n == 0 {
			continue
		}
		if err := writeVerify(backlinkPath, rewritten); err != nil {
			return fail("write backlink %d: %v", src.NoteID, err)
		}
		backlinksUpdated += n
	}

	if err := moveNoteToTrash(srcCfg, srcPath, noteID); err != nil {
		return fail("move source to trash: %v", err)
	}
	if _, err := index.New(srcCfg, srcStore).Full(); err != nil {
		return fail("reindex source: %v", err)
	}
	if _, err := index.New(dstCfg, dstStore).Full(); err != nil {
		return fail("reindex destination: %v", err)
	}
	return emit(map[string]any{
		"id": noteID, "title": meta.Title, "from": srcCfg.VaultName, "to": dstName,
		"path": dstPath, "backlinks_updated": backlinksUpdated,
	})
}

func moveNoteToTrash(cfg *config.Config, notePath string, noteID int64) error {
	if err := os.MkdirAll(cfg.TrashDir(), 0o755); err != nil {
		return fmt.Errorf("create trash dir: %w", err)
	}
	stamp := time.Now().UnixMilli()
	trashedNote := filepath.Join(cfg.TrashDir(), fmt.Sprintf("%d-%s", stamp, filepath.Base(notePath)))
	if err := os.Rename(notePath, trashedNote); err != nil {
		return err
	}
	metaPath := cfg.MetadataPath(noteID)
	if fileExists(metaPath) {
		trashedMeta := filepath.Join(cfg.TrashDir(), fmt.Sprintf("%d-%s", stamp, filepath.Base(metaPath)))
		if err := os.Rename(metaPath, trashedMeta); err != nil {
			// Restore the body to its original path when the sidecar leg fails. If restoration itself
			// fails, the body remains recoverable in trash and the combined error names both locations.
			if restoreErr := os.Rename(trashedNote, notePath); restoreErr != nil {
				return fmt.Errorf("move sidecar: %w; body remains at %s (restore failed: %v)", err, trashedNote, restoreErr)
			}
			return fmt.Errorf("move sidecar: %w", err)
		}
	}
	return nil
}

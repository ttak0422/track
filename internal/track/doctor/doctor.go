// Package doctor runs non-destructive consistency checks over a vault: it compares the note/journal
// markdown files on disk against their sidecar metadata and the id naming rule, surfacing the kinds of
// divergence that cloud sync (e.g. OneDrive) tends to introduce before a reindex would silently act on
// them. Diagnose never modifies the vault; repair is intentionally a separate, opt-in step.
package doctor

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/note"
)

// IssueKind classifies a single divergence found during Diagnose.
type IssueKind string

const (
	// IssueMissingSidecar is a note/journal markdown file with no readable sidecar metadata. The normal
	// create path always writes a sidecar, so a missing one usually means a partially synced vault.
	IssueMissingSidecar IssueKind = "missing_sidecar"
	// IssueOrphanSidecar is a sidecar with no matching markdown file in note/ or journal/. A full
	// reindex deletes these silently; doctor surfaces them first so a sync gap is not mistaken for a delete.
	IssueOrphanSidecar IssueKind = "orphan_sidecar"
	// IssueStrayFile is a file under note/ or journal/ that carries a note extension but does not match
	// the numeric id naming rule, e.g. an OneDrive conflict copy "1781359469000 (conflicted copy).md".
	IssueStrayFile IssueKind = "stray_file"
	// IssueUnreadableSidecar is a sidecar that exists but cannot be parsed (bad YAML or unsupported version).
	IssueUnreadableSidecar IssueKind = "unreadable_sidecar"
	// IssueDuplicateTitle is a title shared by more than one note. Creation prevents this, but a sync
	// merge can reintroduce it, leaving [[title]] links ambiguous.
	IssueDuplicateTitle IssueKind = "duplicate_title"
)

// Issue is one divergence between the on-disk files and their metadata.
type Issue struct {
	Kind   IssueKind `json:"kind"`
	ID     int64     `json:"id,omitempty"`
	Path   string    `json:"path,omitempty"`
	Detail string    `json:"detail,omitempty"`
}

// Report summarizes a Diagnose run. Issues is sorted for stable output; an empty slice means a clean vault.
type Report struct {
	Scanned int     `json:"scanned"`
	Issues  []Issue `json:"issues"`
}

// Diagnose scans the vault and reports anomalies without changing anything on disk. The on-disk
// markdown plus sidecars are treated as the source of truth; the rebuildable index is ignored here.
//
// TODO(track): a follow-up `--fix` pass needs product decisions before it can be safe — what title to
// stamp when recreating a missing sidecar (filename id? prompt?), where to move orphan sidecars and
// stray conflict copies (a quarantine dir under .track?), and how to resolve duplicate titles (auto
// suffix vs. report-only). Keep Diagnose read-only until those are settled.
func Diagnose(cfg *config.Config) (Report, error) {
	var rep Report

	type titled struct {
		id   int64
		path string
	}
	byTitle := map[string][]titled{}
	noteIDs := map[int64]bool{}

	for _, root := range []string{cfg.NoteDir(), cfg.JournalDir()} {
		entries, err := os.ReadDir(root)
		if err != nil {
			if os.IsNotExist(err) {
				continue // note/journal dir may not exist yet
			}
			return rep, err
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			path := filepath.Join(root, e.Name())
			ext := filepath.Ext(e.Name())
			if !containsExt(cfg.Extensions, ext) {
				continue // non-note files (e.g. .DS_Store) are not our concern
			}
			_, ok := cfg.KindFromPath(path)
			if !ok {
				rep.Issues = append(rep.Issues, Issue{Kind: IssueStrayFile, Path: path,
					Detail: "file under " + kind3(root) + " does not match the <id>" + ext + " naming rule"})
				continue
			}
			rep.Scanned++
			id, err := note.IDFromPath(path)
			if err != nil {
				rep.Issues = append(rep.Issues, Issue{Kind: IssueStrayFile, Path: path, Detail: err.Error()})
				continue
			}
			noteIDs[id] = true

			meta, found, err := note.ReadMetadata(cfg.MetadataPath(id))
			switch {
			case err != nil:
				rep.Issues = append(rep.Issues, Issue{Kind: IssueUnreadableSidecar, ID: id,
					Path: cfg.MetadataPath(id), Detail: err.Error()})
			case !found:
				rep.Issues = append(rep.Issues, Issue{Kind: IssueMissingSidecar, ID: id, Path: path,
					Detail: "no sidecar at " + cfg.MetadataPath(id)})
			case meta.Title != "":
				byTitle[meta.Title] = append(byTitle[meta.Title], titled{id: id, path: path})
			}
		}
	}

	// Orphan sidecars: a metadata file whose note/journal markdown no longer exists on disk.
	if sidecars, err := os.ReadDir(cfg.MetadataDir()); err == nil {
		for _, e := range sidecars {
			if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
				continue
			}
			stem := strings.TrimSuffix(e.Name(), ".yaml")
			id, err := strconv.ParseInt(stem, 10, 64)
			if err != nil {
				continue
			}
			if noteIDs[id] {
				continue
			}
			rep.Issues = append(rep.Issues, Issue{Kind: IssueOrphanSidecar, ID: id,
				Path:   filepath.Join(cfg.MetadataDir(), e.Name()),
				Detail: "no markdown file in note/ or journal/ for this id"})
		}
	} else if !os.IsNotExist(err) {
		return rep, err
	}

	for title, group := range byTitle {
		if len(group) < 2 {
			continue
		}
		ids := make([]string, 0, len(group))
		for _, g := range group {
			ids = append(ids, strconv.FormatInt(g.id, 10))
		}
		sort.Strings(ids)
		rep.Issues = append(rep.Issues, Issue{Kind: IssueDuplicateTitle,
			Detail: "title " + strconv.Quote(title) + " is shared by ids " + strings.Join(ids, ", ")})
	}

	sortIssues(rep.Issues)
	if rep.Issues == nil {
		rep.Issues = []Issue{}
	}
	return rep, nil
}

func containsExt(exts []string, ext string) bool {
	for _, e := range exts {
		if e == ext {
			return true
		}
	}
	return false
}

// kind3 names a scanned root for human-readable detail without leaking the absolute path.
func kind3(root string) string {
	return filepath.Base(root) + "/"
}

// sortIssues orders issues by kind, then id, then path for deterministic output.
func sortIssues(issues []Issue) {
	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].Kind != issues[j].Kind {
			return issues[i].Kind < issues[j].Kind
		}
		if issues[i].ID != issues[j].ID {
			return issues[i].ID < issues[j].ID
		}
		return issues[i].Path < issues[j].Path
	})
}

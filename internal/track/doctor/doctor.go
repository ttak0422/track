// Package doctor runs non-destructive consistency checks over a vault: it compares the note/journal
// markdown files on disk against their sidecar metadata and the id naming rule, surfacing the kinds of
// divergence that cloud sync (e.g. OneDrive) tends to introduce before a reindex would silently act on
// them. Diagnose never modifies the vault; Fix is the opt-in repair pass.
package doctor

import (
	"fmt"
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

// scanResult is the raw consistency data both Diagnose and Fix build on, so detection logic lives in
// one place.
type scanResult struct {
	scanned        int
	mdIDs          map[int64]bool   // ids of valid markdown files present on disk
	titles         map[int64]string // id -> non-empty sidecar title, for readable sidecars
	missingSidecar []int64          // md present, sidecar absent
	unreadable     []int64          // md present, sidecar unreadable
	unreadablePath map[int64]string // id -> sidecar path for unreadable sidecars
	strays         []string         // file paths that break the id naming rule
	orphans        []int64          // sidecar ids with no markdown file
}

// scan walks the vault once and gathers every consistency signal Diagnose and Fix need.
func scan(cfg *config.Config) (scanResult, error) {
	res := scanResult{
		mdIDs:          map[int64]bool{},
		titles:         map[int64]string{},
		unreadablePath: map[int64]string{},
	}

	for _, root := range []string{cfg.NoteDir(), cfg.JournalDir()} {
		entries, err := os.ReadDir(root)
		if err != nil {
			if os.IsNotExist(err) {
				continue // note/journal dir may not exist yet
			}
			return res, err
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			path := filepath.Join(root, e.Name())
			if !containsExt(cfg.Extensions, filepath.Ext(e.Name())) {
				continue // non-note files (e.g. .DS_Store) are not our concern
			}
			if _, ok := cfg.KindFromPath(path); !ok {
				res.strays = append(res.strays, path)
				continue
			}
			id, err := note.IDFromPath(path)
			if err != nil {
				res.strays = append(res.strays, path)
				continue
			}
			res.scanned++
			res.mdIDs[id] = true

			meta, found, err := note.ReadMetadata(cfg.MetadataPath(id))
			switch {
			case err != nil:
				res.unreadable = append(res.unreadable, id)
				res.unreadablePath[id] = cfg.MetadataPath(id)
			case !found:
				res.missingSidecar = append(res.missingSidecar, id)
			case meta.Title != "":
				res.titles[id] = meta.Title
			}
		}
	}

	sidecars, err := os.ReadDir(cfg.MetadataDir())
	if err != nil && !os.IsNotExist(err) {
		return res, err
	}
	for _, e := range sidecars {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		id, err := strconv.ParseInt(strings.TrimSuffix(e.Name(), ".yaml"), 10, 64)
		if err != nil {
			continue
		}
		if !res.mdIDs[id] {
			res.orphans = append(res.orphans, id)
		}
	}
	return res, nil
}

// duplicateTitleGroups returns, per shared title, the sorted ids that carry it (only titles with >1 id).
func (r scanResult) duplicateTitleGroups() map[string][]int64 {
	byTitle := map[string][]int64{}
	for id, title := range r.titles {
		byTitle[title] = append(byTitle[title], id)
	}
	dups := map[string][]int64{}
	for title, ids := range byTitle {
		if len(ids) > 1 {
			sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
			dups[title] = ids
		}
	}
	return dups
}

// Diagnose scans the vault and reports anomalies without changing anything on disk. The on-disk
// markdown plus sidecars are treated as the source of truth; the rebuildable index is ignored here.
func Diagnose(cfg *config.Config) (Report, error) {
	res, err := scan(cfg)
	if err != nil {
		return Report{}, err
	}
	rep := Report{Scanned: res.scanned}

	for _, id := range res.missingSidecar {
		rep.Issues = append(rep.Issues, Issue{Kind: IssueMissingSidecar, ID: id, Path: cfg.PathForKind(config.KindNote, id),
			Detail: "no sidecar at " + cfg.MetadataPath(id)})
	}
	for _, id := range res.unreadable {
		rep.Issues = append(rep.Issues, Issue{Kind: IssueUnreadableSidecar, ID: id, Path: res.unreadablePath[id],
			Detail: "sidecar exists but could not be parsed"})
	}
	for _, path := range res.strays {
		rep.Issues = append(rep.Issues, Issue{Kind: IssueStrayFile, Path: path,
			Detail: "file does not match the <id> naming rule"})
	}
	for _, id := range res.orphans {
		rep.Issues = append(rep.Issues, Issue{Kind: IssueOrphanSidecar, ID: id, Path: cfg.MetadataPath(id),
			Detail: "no markdown file in note/ or journal/ for this id"})
	}
	for title, ids := range res.duplicateTitleGroups() {
		strs := make([]string, len(ids))
		for i, id := range ids {
			strs[i] = strconv.FormatInt(id, 10)
		}
		rep.Issues = append(rep.Issues, Issue{Kind: IssueDuplicateTitle,
			Detail: "title " + strconv.Quote(title) + " is shared by ids " + strings.Join(strs, ", ")})
	}

	sortIssues(rep.Issues)
	if rep.Issues == nil {
		rep.Issues = []Issue{}
	}
	return rep, nil
}

// FixAction records one repair Fix performed.
type FixAction struct {
	Kind   IssueKind `json:"kind"`
	ID     int64     `json:"id,omitempty"`
	Path   string    `json:"path,omitempty"`
	Detail string    `json:"detail"`
}

// FixReport summarizes a Fix run. Changed reports whether the vault was modified.
type FixReport struct {
	Changed bool        `json:"changed"`
	Fixed   []FixAction `json:"fixed"`
	Skipped []Issue     `json:"skipped"`
}

// Fix repairs the divergence Diagnose finds, restoring with auto-numbered titles/ids:
//
//   - missing_sidecar: write a sidecar with a fresh auto-numbered title.
//   - orphan_sidecar:  recreate the missing markdown as an empty note (its title is already in the sidecar).
//   - duplicate_title: keep the lowest id's title; renumber the rest to fresh auto-numbered titles.
//   - stray_file:      import the file as a new note with a fresh auto-numbered id and title.
//
// unreadable_sidecar is not auto-fixable (the intended contents are unknown) and is reported under
// Skipped. startID seeds the id allocator for stray imports (callers pass a time-based id); ids are then
// taken with note.FreeID so they never collide with files written earlier in the same run.
func Fix(cfg *config.Config, startID int64) (FixReport, error) {
	res, err := scan(cfg)
	if err != nil {
		return FixReport{}, err
	}
	var rep FixReport
	titles := newTitleAllocator(res.titles)

	// missing_sidecar: stamp a fresh title so the note is resolvable again.
	for _, id := range sortedInt64(res.missingSidecar) {
		title := titles.next()
		if err := note.WriteMetadata(cfg.MetadataPath(id), note.Metadata{Title: title}); err != nil {
			return rep, fmt.Errorf("restore sidecar %d: %w", id, err)
		}
		rep.Fixed = append(rep.Fixed, FixAction{Kind: IssueMissingSidecar, ID: id, Path: cfg.MetadataPath(id),
			Detail: "wrote sidecar with title " + strconv.Quote(title)})
	}

	// orphan_sidecar: bring back the note body so the sidecar is no longer dangling.
	for _, id := range sortedInt64(res.orphans) {
		path := cfg.NotePath(id)
		if err := os.MkdirAll(cfg.NoteDir(), 0o755); err != nil {
			return rep, err
		}
		if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
			return rep, fmt.Errorf("restore note %d: %w", id, err)
		}
		rep.Fixed = append(rep.Fixed, FixAction{Kind: IssueOrphanSidecar, ID: id, Path: path,
			Detail: "recreated empty markdown for orphan sidecar"})
	}

	// duplicate_title: keep the lowest id, renumber the rest.
	for title, ids := range res.duplicateTitleGroups() {
		for _, id := range ids[1:] {
			meta, _, err := note.ReadMetadata(cfg.MetadataPath(id))
			if err != nil {
				return rep, fmt.Errorf("read sidecar %d: %w", id, err)
			}
			newTitle := titles.next()
			meta.Title = newTitle
			if err := note.WriteMetadata(cfg.MetadataPath(id), meta); err != nil {
				return rep, fmt.Errorf("renumber title %d: %w", id, err)
			}
			rep.Fixed = append(rep.Fixed, FixAction{Kind: IssueDuplicateTitle, ID: id, Path: cfg.MetadataPath(id),
				Detail: "renamed duplicate of " + strconv.Quote(title) + " to " + strconv.Quote(newTitle)})
		}
	}

	// stray_file: import each as a fresh note with a new id and title.
	next := startID
	for _, path := range res.strays {
		id, err := note.FreeID(cfg, next)
		if err != nil {
			return rep, fmt.Errorf("allocate id for %s: %w", path, err)
		}
		next = id + 1
		dest := cfg.NotePath(id)
		if err := os.MkdirAll(cfg.NoteDir(), 0o755); err != nil {
			return rep, err
		}
		if err := os.Rename(path, dest); err != nil {
			return rep, fmt.Errorf("import stray %s: %w", path, err)
		}
		title := titles.next()
		if err := note.WriteMetadata(cfg.MetadataPath(id), note.Metadata{Title: title}); err != nil {
			return rep, fmt.Errorf("write sidecar for imported %d: %w", id, err)
		}
		rep.Fixed = append(rep.Fixed, FixAction{Kind: IssueStrayFile, ID: id, Path: dest,
			Detail: "imported " + filepath.Base(path) + " as note " + strconv.FormatInt(id, 10) + " titled " + strconv.Quote(title)})
	}

	for _, id := range sortedInt64(res.unreadable) {
		rep.Skipped = append(rep.Skipped, Issue{Kind: IssueUnreadableSidecar, ID: id, Path: res.unreadablePath[id],
			Detail: "cannot auto-repair an unreadable sidecar; fix or remove it by hand"})
	}

	rep.Changed = len(rep.Fixed) > 0
	if rep.Fixed == nil {
		rep.Fixed = []FixAction{}
	}
	if rep.Skipped == nil {
		rep.Skipped = []Issue{}
	}
	return rep, nil
}

// titleAllocator hands out unique "Untitled N" titles, skipping any already in use.
type titleAllocator struct {
	used map[string]bool
	n    int
}

func newTitleAllocator(titles map[int64]string) *titleAllocator {
	used := map[string]bool{}
	for _, t := range titles {
		used[t] = true
	}
	return &titleAllocator{used: used}
}

func (a *titleAllocator) next() string {
	for {
		a.n++
		candidate := "Untitled " + strconv.Itoa(a.n)
		if !a.used[candidate] {
			a.used[candidate] = true
			return candidate
		}
	}
}

func containsExt(exts []string, ext string) bool {
	for _, e := range exts {
		if e == ext {
			return true
		}
	}
	return false
}

func sortedInt64(in []int64) []int64 {
	out := append([]int64(nil), in...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
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

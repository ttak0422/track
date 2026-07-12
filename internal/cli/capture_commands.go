package cli

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/link"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
	tmpl "github.com/ttak0422/track/internal/track/template"
)

// cmdCapture appends a (templated) entry under a heading anchor of a target note. The target is a
// "<note>#<heading>" string; with --target omitted it falls back to the configured capture inbox,
// which is created on first use. A named heading must already resolve unambiguously — capture never
// invents a heading. With --template, the captured text fills the template's {{ title }} placeholder.
func cmdCapture(args []string) int {
	fs := flag.NewFlagSet("capture", flag.ContinueOnError)
	target := fs.String("target", "", `destination "<note>#<heading>"; defaults to the configured capture inbox`)
	template := fs.String("template", "", "template applied to the captured text ({{ title }} is the text)")
	bodyFlag := fs.String("body", "", "text to capture; read from stdin when omitted and piped")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}

	text, err := readBody(fs, *bodyFlag)
	if err != nil {
		return fail("read body: %v", err)
	}
	text = strings.TrimRight(text, "\n")
	if strings.TrimSpace(text) == "" {
		return fail("nothing to capture: provide --body (or piped stdin)")
	}

	cfg, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	tgt := strings.TrimSpace(*target)
	explicit := tgt != ""
	if tgt == "" {
		tgt = cfg.CaptureInbox
	}
	if tgt == "" {
		return fail("no --target and no capture inbox configured")
	}
	key, heading, level := link.SplitAnchor(tgt)
	if key == "" {
		return fail("invalid target %q", tgt)
	}

	// An explicit target must already exist; the configured inbox is created on first capture.
	var notePath string
	var noteID int64
	if explicit {
		notePath, err = resolveNotePath(cfg, s, 0, key, "")
		if err != nil {
			return fail("%v", err)
		}
		noteID, err = note.IDFromPath(notePath)
		if err != nil {
			return fail("invalid note path: %v", err)
		}
	} else if notePath, noteID, err = ensureNote(cfg, s, key); err != nil {
		return fail("%v", err)
	}

	body, err := os.ReadFile(notePath)
	if err != nil {
		return fail("read note: %v", err)
	}
	hp, err := resolveHeadingPtr(string(body), heading, level)
	if err != nil {
		return fail("%v", err)
	}

	entry := text
	if strings.TrimSpace(*template) != "" {
		rendered, err := tmpl.Render(cfg, *template, text, noteID, config.KindNote, "", time.Now())
		if err != nil {
			return fail("render template: %v", err)
		}
		entry = strings.TrimRight(rendered, "\n")
	}
	newBody, at := link.AppendUnder(string(body), hp, strings.Split(entry, "\n"))
	if at == 0 {
		return fail("captured text is empty after templating")
	}
	if err := writeVerify(notePath, newBody); err != nil {
		return fail("%v", err)
	}
	if err := index.New(cfg, s).One(notePath); err != nil {
		return fail("index note: %v", err)
	}
	return emit(map[string]any{"id": noteID, "path": notePath, "target": tgt, "line": at})
}

// cmdRefile moves a heading subtree — or, with --line, a single list item — from one note anchor to
// another. Text moves verbatim, so [[...]] links inside it keep resolving; both endpoints are reindexed
// so the link graph follows. Across notes the destination is written and verified before the source is
// touched, so a failed write never loses the moved text.
func cmdRefile(args []string) int {
	fs := flag.NewFlagSet("refile", flag.ContinueOnError)
	from := fs.String("from", "", `source "<note>#<heading>" (or just "<note>" with --line)`)
	to := fs.String("to", "", `destination "<note>#<heading>" (or "<note>" to append at the end)`)
	line := fs.Int("line", 0, "move the single list item at this 1-based line of the source note")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}

	fromKey, fromHeading, fromLevel := link.SplitAnchor(strings.TrimSpace(*from))
	toKey, toHeading, toLevel := link.SplitAnchor(strings.TrimSpace(*to))
	if fromKey == "" {
		return fail("--from is required")
	}
	if toKey == "" {
		return fail("--to is required")
	}
	usingLine := flagWasSet(fs, "line")
	if usingLine && fromHeading != "" {
		return fail("--from must not carry a #heading when --line is used")
	}
	if !usingLine && fromHeading == "" {
		return fail(`--from needs a "#heading" (or pass --line for a single list item)`)
	}

	cfg, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	fromPath, err := resolveNotePath(cfg, s, 0, fromKey, "")
	if err != nil {
		return fail("%v", err)
	}
	toPath, err := resolveNotePath(cfg, s, 0, toKey, "")
	if err != nil {
		return fail("%v", err)
	}

	raw, err := os.ReadFile(fromPath)
	if err != nil {
		return fail("read source: %v", err)
	}
	fromBody := string(raw)

	var rest string
	var block []string
	if usingLine {
		rest, block, err = link.CutListItem(fromBody, *line)
	} else {
		var h link.Heading
		if h, err = link.ResolveAnchor(fromBody, fromHeading, fromLevel); err == nil {
			rest, block = link.CutSection(fromBody, h)
		}
	}
	if err != nil {
		return fail("%v", err)
	}
	if len(block) == 0 {
		return fail("nothing to move at the source anchor")
	}

	if fromPath == toPath {
		// Same note: append into the post-cut body and re-resolve the destination heading against it,
		// since cutting the source shifts every line below it.
		hp, err := resolveHeadingPtr(rest, toHeading, toLevel)
		if err != nil {
			return fail("%v", err)
		}
		newBody, at := link.AppendUnder(rest, hp, block)
		if err := writeVerify(fromPath, newBody); err != nil {
			return fail("%v", err)
		}
		if err := index.New(cfg, s).One(fromPath); err != nil {
			return fail("index note: %v", err)
		}
		return emit(map[string]any{"from": *from, "to": *to, "moved": len(block), "line": at, "same_note": true})
	}

	toRaw, err := os.ReadFile(toPath)
	if err != nil {
		return fail("read destination: %v", err)
	}
	hp, err := resolveHeadingPtr(string(toRaw), toHeading, toLevel)
	if err != nil {
		return fail("%v", err)
	}
	newTo, at := link.AppendUnder(string(toRaw), hp, block)
	if err := writeVerify(toPath, newTo); err != nil {
		return fail("write destination: %v", err)
	}
	// Destination now holds the text; only then remove it from the source. A failure here duplicates
	// rather than loses, and is surfaced so the user can reconcile.
	if err := writeVerify(fromPath, rest); err != nil {
		return fail("moved into destination but failed to update source (text is now in both): %v", err)
	}
	ix := index.New(cfg, s)
	if err := ix.One(toPath); err != nil {
		return fail("index destination: %v", err)
	}
	if err := ix.One(fromPath); err != nil {
		return fail("index source: %v", err)
	}
	return emit(map[string]any{"from": *from, "to": *to, "moved": len(block), "line": at, "same_note": false})
}

// cmdArchive moves a heading subtree out of its note into the configured archive note (per-year by
// default), stamping the moved content with a provenance line — a [[link]] back to the source and the
// date — so where it came from survives the move. Unlike rm this is a living move, not a deletion.
func cmdArchive(args []string) int {
	fs := flag.NewFlagSet("archive", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}
	target := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if target == "" {
		return fail(`usage: track archive "<note>#<heading>"`)
	}
	key, heading, level := link.SplitAnchor(target)
	if key == "" || heading == "" {
		return fail(`archive target must be "<note>#<heading>"`)
	}

	cfg, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	srcPath, err := resolveNotePath(cfg, s, 0, key, "")
	if err != nil {
		return fail("%v", err)
	}
	srcID, err := note.IDFromPath(srcPath)
	if err != nil {
		return fail("invalid note path: %v", err)
	}
	meta, _, err := note.ReadMetadata(cfg.MetadataPath(srcID))
	if err != nil {
		return fail("read metadata: %v", err)
	}

	raw, err := os.ReadFile(srcPath)
	if err != nil {
		return fail("read source: %v", err)
	}
	h, err := link.ResolveAnchor(string(raw), heading, level)
	if err != nil {
		return fail("%v", err)
	}
	rest, block := link.CutSection(string(raw), h)

	now := time.Now()
	block = withProvenance(block, meta.Title, now.Format(cfg.DateFormat))

	archiveTitle := cfg.ArchiveNoteTitle(now)
	archivePath, _, err := ensureNote(cfg, s, archiveTitle)
	if err != nil {
		return fail("%v", err)
	}
	if archivePath == srcPath {
		return fail("cannot archive a section into its own note (%q)", archiveTitle)
	}

	archiveRaw, err := os.ReadFile(archivePath)
	if err != nil {
		return fail("read archive: %v", err)
	}
	newArchive, at := link.AppendUnder(string(archiveRaw), nil, block)
	if err := writeVerify(archivePath, newArchive); err != nil {
		return fail("write archive: %v", err)
	}
	if err := writeVerify(srcPath, rest); err != nil {
		return fail("moved into archive but failed to update source (text is now in both): %v", err)
	}
	ix := index.New(cfg, s)
	if err := ix.One(archivePath); err != nil {
		return fail("index archive: %v", err)
	}
	if err := ix.One(srcPath); err != nil {
		return fail("index source: %v", err)
	}
	return emit(map[string]any{
		"id":           srcID,
		"source":       meta.Title,
		"archive":      archiveTitle,
		"archive_path": archivePath,
		"line":         at,
	})
}

// withProvenance inserts an archival note directly under the moved section's heading: a [[link]] back to
// the source note (by title) and the date. When the source has no title (no sidecar), the link is
// dropped so no broken [[]] is written.
func withProvenance(block []string, sourceTitle, date string) []string {
	var prov string
	if strings.TrimSpace(sourceTitle) == "" {
		prov = fmt.Sprintf("*Archived on %s.*", date)
	} else {
		prov = fmt.Sprintf("*Archived from [[%s]] on %s.*", sourceTitle, date)
	}
	out := make([]string, 0, len(block)+2)
	out = append(out, block[0], "", prov)
	out = append(out, block[1:]...)
	return out
}

// resolveHeadingPtr resolves an optional heading anchor within body. An empty heading yields a nil
// pointer, meaning "the whole note" (append at its end); a named heading must resolve unambiguously.
func resolveHeadingPtr(body, heading string, level int) (*link.Heading, error) {
	if heading == "" {
		return nil, nil
	}
	h, err := link.ResolveAnchor(body, heading, level)
	if err != nil {
		return nil, err
	}
	return &h, nil
}

// ensureNote resolves a note by title, creating an empty note (default template) when none exists.
// The note is indexed on creation, so its title resolves immediately afterward.
func ensureNote(cfg *config.Config, s *store.Store, title string) (path string, id int64, err error) {
	ref, found, err := s.ResolveTerm(title)
	if err != nil {
		return "", 0, fmt.Errorf("resolve: %v", err)
	}
	if found {
		return cfg.PathForKind(ref.FileKind, ref.NoteID), ref.NoteID, nil
	}
	newID, err := note.NewID(cfg, time.Now())
	if err != nil {
		return "", 0, fmt.Errorf("allocate note id: %v", err)
	}
	res, err := createTitledNote(cfg, s, newID, title, "", "", nil, "")
	if err != nil {
		return "", 0, err
	}
	return res["path"].(string), newID, nil
}

// writeVerify writes content to path and reads it back, confirming the exact bytes landed before any
// caller proceeds to mutate a second file. This is the "never lose text" guard behind refile/archive:
// the destination is verified present before the source is stripped.
func writeVerify(path, content string) error {
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write %s: %v", path, err)
	}
	back, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("verify %s: %v", path, err)
	}
	if string(back) != content {
		return fmt.Errorf("write verification failed for %s", path)
	}
	return nil
}

package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/journal"
	"github.com/ttak0422/track/internal/track/link"
	"github.com/ttak0422/track/internal/track/note"
	trackrename "github.com/ttak0422/track/internal/track/rename"
	"github.com/ttak0422/track/internal/track/store"
	tmpl "github.com/ttak0422/track/internal/track/template"
)

func cmdNew(args []string) int {
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	title := fs.String("title", "", "note title stored in metadata and used as a link keyword")
	id := fs.Int64("id", 0, "note id; defaults to current Unix second * 1000 plus a same-second sequence")
	template := fs.String("template", "", "template name or path")
	parentPath := fs.String("parent-path", "", "path of the note this creation was triggered from; its title fills the template {{ parent }}")
	bodyFlag := fs.String("body", "", "note body; read from stdin when omitted and piped")
	var tags tagsFlag
	fs.Var(&tags, "tag", "tag to attach (repeatable, comma-separated)")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}
	t := strings.TrimSpace(*title)
	if t == "" {
		return fail("--title is required")
	}

	body, err := readBody(fs, *bodyFlag)
	if err != nil {
		return fail("read body: %v", err)
	}
	if strings.TrimSpace(*template) != "" && strings.TrimSpace(body) != "" {
		return fail("--body cannot be combined with --template")
	}

	cfg, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	// Titles are link keywords, so a second note with the same title would make the keyword
	// ambiguous. new is the strict create: it refuses an existing title rather than minting a
	// duplicate. Use open to create-or-open idempotently.
	if _, found, err := s.ResolveTerm(t); err != nil {
		return fail("resolve: %v", err)
	} else if found {
		return fail("note already exists for title %q", t)
	}

	noteID := *id
	if noteID == 0 {
		noteID, err = note.NewID(cfg, time.Now())
		if err != nil {
			return fail("allocate note id: %v", err)
		}
	}

	res, err := createTitledNote(cfg, s, noteID, t, *template, body, dedupTags(tags), parentTitleFromPath(cfg, *parentPath))
	if err != nil {
		return fail("%v", err)
	}
	return emit(res)
}

// cmdOpen resolves a title to its note, creating one only when none exists. Because it never makes a
// second note for a title that already resolves, repeated opens keep titles unique. The result carries
// "created" so callers can decide whether a reindex (to pick up new inbound links) is needed.
func cmdOpen(args []string) int {
	fs := flag.NewFlagSet("open", flag.ContinueOnError)
	title := fs.String("title", "", "note title to open, or create when absent (JSON)")
	template := fs.String("template", "", "template name or path used when creating")
	parentPath := fs.String("parent-path", "", "path of the note this creation was triggered from; its title fills the template {{ parent }}")
	bodyFlag := fs.String("body", "", "body used when creating; read from stdin when omitted and piped")
	var tags tagsFlag
	fs.Var(&tags, "tag", "tag attached when creating (repeatable, comma-separated)")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}
	t := strings.TrimSpace(*title)
	if t == "" {
		return fail("--title is required")
	}

	body, err := readBody(fs, *bodyFlag)
	if err != nil {
		return fail("read body: %v", err)
	}
	noteTags := dedupTags(tags)
	hasContent := strings.TrimSpace(body) != "" || len(noteTags) > 0
	if strings.TrimSpace(*template) != "" && strings.TrimSpace(body) != "" {
		return fail("--body cannot be combined with --template")
	}

	cfg, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	ref, found, err := s.ResolveTerm(t)
	if err != nil {
		return fail("resolve: %v", err)
	}
	if found {
		// open is create-or-open; content flags only apply on creation. Adding to an existing note
		// is append's job, so surface that rather than silently dropping --body/--tag.
		if hasContent {
			return fail("note %q already exists; use `track append` to add content or tags", t)
		}
		return emit(map[string]any{"id": ref.NoteID, "path": cfg.PathForKind(ref.FileKind, ref.NoteID), "title": t, "created": false})
	}

	noteID, err := note.NewID(cfg, time.Now())
	if err != nil {
		return fail("allocate note id: %v", err)
	}
	res, err := createTitledNote(cfg, s, noteID, t, *template, body, noteTags, parentTitleFromPath(cfg, *parentPath))
	if err != nil {
		return fail("%v", err)
	}
	res["created"] = true
	return emit(res)
}

// parentTitleFromPath resolves a parent note's title from its file path, for the template {{ parent }}
// substitution. It is best-effort: an empty path, a non-note path, or missing metadata yields "".
func parentTitleFromPath(cfg *config.Config, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if _, ok := cfg.KindFromPath(path); !ok {
		return ""
	}
	id, err := note.IDFromPath(path)
	if err != nil {
		return ""
	}
	meta, found, err := note.ReadMetadata(cfg.MetadataPath(id))
	if err != nil || !found {
		return ""
	}
	return meta.Title
}

// createTitledNote writes a new note titled `title` at `noteID`, indexes it, and returns its summary.
// It guards against clobbering an existing file so an explicit id collision surfaces as an error.
// extraBody is written as the note body without injecting a title heading; it is mutually exclusive
// with template (callers enforce that). tags are stored on the sidecar metadata.
func createTitledNote(cfg *config.Config, s *store.Store, noteID int64, title string, template string, extraBody string, tags []string, parent string) (map[string]any, error) {
	path := cfg.NotePath(noteID)
	if _, err := os.Stat(path); err == nil {
		return nil, fmt.Errorf("note already exists: %s", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create note dir: %v", err)
	}
	body := ""
	effectiveTemplate := strings.TrimSpace(template)
	if effectiveTemplate == "" && strings.TrimSpace(extraBody) == "" {
		def, err := tmpl.DefaultSpec(cfg, config.KindNote)
		if err != nil {
			return nil, fmt.Errorf("resolve default template: %v", err)
		}
		effectiveTemplate = def
	}
	if effectiveTemplate != "" {
		rendered, err := tmpl.Render(cfg, effectiveTemplate, title, noteID, config.KindNote, parent, time.Now())
		if err != nil {
			return nil, fmt.Errorf("render template: %v", err)
		}
		body = ensureTrailingNewline(rendered)
	} else if strings.TrimSpace(extraBody) != "" {
		body = ensureTrailingNewline(extraBody)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return nil, fmt.Errorf("write note: %v", err)
	}
	if err := note.WriteMetadata(
		cfg.MetadataPath(noteID),
		note.Metadata{Title: title, Tags: tags, Created: time.Now().Format(cfg.DateFormat)},
	); err != nil {
		return nil, fmt.Errorf("write metadata: %v", err)
	}
	if err := index.New(cfg, s).One(path); err != nil {
		return nil, fmt.Errorf("index note: %v", err)
	}
	return map[string]any{"id": noteID, "path": path, "title": title}, nil
}

func cmdJournal(args []string) int {
	fs := flag.NewFlagSet("journal", flag.ContinueOnError)
	offset := fs.Int("offset", 0, "day offset: 0=today, -1=yesterday, 1=tomorrow")
	template := fs.String("template", "", "template name or path used when creating")
	bodyFlag := fs.String("body", "", "body used when creating; read from stdin when omitted and piped")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}

	body, err := readBody(fs, *bodyFlag)
	if err != nil {
		return fail("read body: %v", err)
	}
	if strings.TrimSpace(*template) != "" && strings.TrimSpace(body) != "" {
		return fail("--body cannot be combined with --template")
	}

	cfg, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	day := time.Now().AddDate(0, 0, *offset)
	// The journal engine owns file/sidecar/summaries; the CLI keeps template policy by rendering the
	// creation body here (honoring an explicit --template, --body, or the configured/builtin default) and
	// indexes whatever the engine reports as changed.
	res, err := journal.Open(cfg, day, journal.Options{
		CreateBody: func(name string, id int64, d time.Time) (string, error) {
			effectiveTemplate := strings.TrimSpace(*template)
			if effectiveTemplate == "" && strings.TrimSpace(body) == "" {
				def, err := tmpl.DefaultSpec(cfg, config.KindJournal)
				if err != nil {
					return "", err
				}
				effectiveTemplate = def
			}
			if effectiveTemplate != "" {
				return tmpl.Render(cfg, effectiveTemplate, name, id, config.KindJournal, "", d)
			}
			return body, nil
		},
	})
	if err != nil {
		return fail("journal: %v", err)
	}
	ix := index.New(cfg, s)
	for _, p := range res.Reindex {
		if err := ix.One(p); err != nil {
			return fail("index journal: %v", err)
		}
	}
	if !res.Created && strings.TrimSpace(body) != "" {
		// The journal existed already; content flags only apply on creation. Point to append so the
		// daily-log workflow has an explicit path rather than silently dropping the body.
		return fail("journal %s already exists; use `track append --id %d` to add content or tags", res.Name, res.NoteID)
	}

	return emit(map[string]any{"id": res.NoteID, "path": res.Path, "created": res.Created})
}

// cmdAppend grows an existing note: it appends body text and/or merges tags, then reindexes the note
// so its links and provenance are reflected without a full rebuild. The target is named by one of
// --id, --title, or --path. This is the create-or-open counterpart for editing notes through the CLI.
func cmdAppend(args []string) int {
	fs := flag.NewFlagSet("append", flag.ContinueOnError)
	id := fs.Int64("id", 0, "note id")
	title := fs.String("title", "", "note title (alternative to --id)")
	path := fs.String("path", "", "note path (alternative to --id)")
	bodyFlag := fs.String("body", "", "text to append; read from stdin when omitted and piped")
	var tags tagsFlag
	fs.Var(&tags, "tag", "tag to add (repeatable, comma-separated)")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}

	appendText, err := readBody(fs, *bodyFlag)
	if err != nil {
		return fail("read body: %v", err)
	}
	addTags := dedupTags(tags)
	if strings.TrimSpace(appendText) == "" && len(addTags) == 0 {
		return fail("nothing to do: provide --body (or piped stdin) or --tag")
	}

	cfg, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	notePath, err := resolveNotePath(cfg, s, *id, strings.TrimSpace(*title), strings.TrimSpace(*path))
	if err != nil {
		return fail("%v", err)
	}
	noteID, err := note.IDFromPath(notePath)
	if err != nil {
		return fail("invalid note path: %v", err)
	}

	if appendText != "" {
		existing, err := os.ReadFile(notePath)
		if err != nil {
			return fail("read note: %v", err)
		}
		if err := os.WriteFile(notePath, []byte(appendBody(string(existing), appendText)), 0o644); err != nil {
			return fail("write note: %v", err)
		}
	}

	if len(addTags) > 0 {
		meta, found, err := note.ReadMetadata(cfg.MetadataPath(noteID))
		if err != nil {
			return fail("read metadata: %v", err)
		}
		if !found {
			// A note without a sidecar is unusual, but reconstruct enough to keep the schema valid
			// rather than failing the append outright.
			meta = note.Metadata{Created: time.Now().Format(cfg.DateFormat)}
		}
		meta.Tags = dedupTags(append(meta.Tags, addTags...))
		if err := note.WriteMetadata(cfg.MetadataPath(noteID), meta); err != nil {
			return fail("write metadata: %v", err)
		}
	}

	if err := index.New(cfg, s).One(notePath); err != nil {
		return fail("index note: %v", err)
	}
	return emit(map[string]any{"id": noteID, "path": notePath})
}

// cmdUpdate replaces an existing note body and/or updates its tags, then reindexes the note so search,
// outgoing links, backlinks, and activity days reflect the new content without a full rebuild. Title
// changes remain the job of rename because they have backlink-rewrite semantics.
func cmdUpdate(args []string) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	id := fs.Int64("id", 0, "note id")
	title := fs.String("title", "", "note title (alternative to --id)")
	path := fs.String("path", "", "note path (alternative to --id)")
	bodyFlag := fs.String("body", "", "replacement body; read from stdin when omitted and piped")
	clearTags := fs.Bool("clear-tags", false, "remove existing tags before applying --tag")
	var tags tagsFlag
	fs.Var(&tags, "tag", "tag to add (repeatable, comma-separated)")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}

	bodyWasSet := flagWasSet(fs, "body")
	updateText, err := readBody(fs, *bodyFlag)
	if err != nil {
		return fail("read body: %v", err)
	}
	if updateText != "" {
		bodyWasSet = true
	}
	addTags := dedupTags(tags)
	if !bodyWasSet && len(addTags) == 0 && !*clearTags {
		return fail("nothing to do: provide --body (or piped stdin), --tag, or --clear-tags")
	}

	cfg, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	notePath, err := resolveNotePath(cfg, s, *id, strings.TrimSpace(*title), strings.TrimSpace(*path))
	if err != nil {
		return fail("%v", err)
	}
	noteID, err := note.IDFromPath(notePath)
	if err != nil {
		return fail("invalid note path: %v", err)
	}

	if bodyWasSet {
		if err := os.WriteFile(notePath, []byte(ensureTrailingNewline(updateText)), 0o644); err != nil {
			return fail("write note: %v", err)
		}
	}

	tagsUpdated := len(addTags) > 0 || *clearTags
	if tagsUpdated {
		meta, found, err := note.ReadMetadata(cfg.MetadataPath(noteID))
		if err != nil {
			return fail("read metadata: %v", err)
		}
		if !found {
			meta = note.Metadata{Created: time.Now().Format(cfg.DateFormat)}
		}
		if *clearTags {
			meta.Tags = nil
		}
		meta.Tags = dedupTags(append(meta.Tags, addTags...))
		if err := note.WriteMetadata(cfg.MetadataPath(noteID), meta); err != nil {
			return fail("write metadata: %v", err)
		}
	}

	if err := index.New(cfg, s).One(notePath); err != nil {
		return fail("index note: %v", err)
	}
	return emit(map[string]any{
		"id":           noteID,
		"path":         notePath,
		"body_updated": bodyWasSet,
		"tags_updated": tagsUpdated,
	})
}

func cmdRename(args []string) int {
	fs := flag.NewFlagSet("rename", flag.ContinueOnError)
	id := fs.Int64("id", 0, "note id")
	title := fs.String("title", "", "note title (alternative to --id)")
	path := fs.String("path", "", "note path (alternative to --id)")
	toFlag := fs.String("to", "", "new note title")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}
	to := strings.TrimSpace(*toFlag)
	if to == "" {
		return fail("--to is required")
	}

	cfg, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	notePath, err := resolveNotePath(cfg, s, *id, strings.TrimSpace(*title), strings.TrimSpace(*path))
	if err != nil {
		return fail("%v", err)
	}
	noteID, err := note.IDFromPath(notePath)
	if err != nil {
		return fail("invalid note path: %v", err)
	}

	meta, found, err := note.ReadMetadata(cfg.MetadataPath(noteID))
	if err != nil {
		return fail("read metadata: %v", err)
	}
	if !found {
		return fail("no metadata for note %d", noteID)
	}
	oldTitle := meta.Title
	if oldTitle == to {
		return emit(map[string]any{"id": noteID, "path": notePath, "old_title": oldTitle, "new_title": to, "backlinks_updated": 0})
	}
	if ref, ok, err := s.ResolveTerm(to); err != nil {
		return fail("resolve: %v", err)
	} else if ok && ref.NoteID != noteID {
		return fail("title %q already in use by note %d", to, ref.NoteID)
	}

	backlinks, err := s.Backlinks(noteID)
	if err != nil {
		return fail("backlinks: %v", err)
	}
	updated := 0
	for _, src := range backlinks {
		srcPath := cfg.PathForKind(src.FileKind, src.NoteID)
		raw, err := os.ReadFile(srcPath)
		if err != nil {
			return fail("read backlink %d: %v", src.NoteID, err)
		}
		rewritten, n := link.ReplaceRefKey(string(raw), oldTitle, to)
		if n == 0 {
			continue
		}
		if err := os.WriteFile(srcPath, []byte(rewritten), 0o644); err != nil {
			return fail("write backlink %d: %v", src.NoteID, err)
		}
		updated += n
	}

	meta.Title = to
	if err := note.WriteMetadata(cfg.MetadataPath(noteID), meta); err != nil {
		return fail("write metadata: %v", err)
	}
	if err := trackrename.Append(cfg.RenamesPath(), trackrename.Entry{From: oldTitle, To: to, NoteID: noteID}); err != nil {
		return fail("write rename history: %v", err)
	}
	if _, err := index.New(cfg, s).Full(); err != nil {
		return fail("reindex: %v", err)
	}
	return emit(map[string]any{"id": noteID, "path": notePath, "old_title": oldTitle, "new_title": to, "backlinks_updated": updated})
}

// resolveNotePath turns one of --id/--title/--path into a concrete, existing note file path.
func resolveNotePath(cfg *config.Config, s *store.Store, id int64, title, path string) (string, error) {
	switch {
	case path != "":
		if _, ok := cfg.KindFromPath(path); !ok {
			return "", fmt.Errorf("path is not a vault note: %s", path)
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		if !fileExists(abs) {
			return "", fmt.Errorf("note not found: %s", abs)
		}
		return abs, nil
	case title != "":
		ref, found, err := s.ResolveTerm(title)
		if err != nil {
			return "", fmt.Errorf("resolve: %v", err)
		}
		if !found {
			return "", fmt.Errorf("no note titled %q", title)
		}
		return cfg.PathForKind(ref.FileKind, ref.NoteID), nil
	case id != 0:
		// A bare id does not carry its kind; journal ids equal their yyyyMMdd name, so probe both layouts.
		if p := cfg.NotePath(id); fileExists(p) {
			return p, nil
		}
		if p := cfg.JournalPath(strconv.FormatInt(id, 10)); fileExists(p) {
			return p, nil
		}
		return "", fmt.Errorf("no note with id %d", id)
	default:
		return "", fmt.Errorf("--id, --title, or --path is required")
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// appendBody appends text to an existing note body, separated by a blank line and ending in a newline.
func appendBody(existing, text string) string {
	base := strings.TrimRight(existing, "\n")
	add := strings.TrimRight(text, "\n")
	if base == "" {
		return add + "\n"
	}
	return base + "\n\n" + add + "\n"
}

func ensureTrailingNewline(body string) string {
	if body == "" || strings.HasSuffix(body, "\n") {
		return body
	}
	return body + "\n"
}

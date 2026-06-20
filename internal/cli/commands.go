package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/doctor"
	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/link"
	"github.com/ttak0422/track/internal/track/note"
	trackrename "github.com/ttak0422/track/internal/track/rename"
	"github.com/ttak0422/track/internal/track/store"
	"github.com/ttak0422/track/internal/track/webui"
)

// cmdInit creates the vault directory skeleton (note/journal trees with their assets subdirectories,
// the template directory, and the sidecar metadata directory). It is idempotent and reports the
// directories it created, so it is safe to run on an existing vault.
func cmdInit(args []string) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		return fail("%v", err)
	}
	created, err := cfg.EnsureVaultSkeleton()
	if err != nil {
		return fail("%v", err)
	}
	if created == nil {
		created = []string{}
	}
	return emit(map[string]any{"vault": cfg.VaultDir, "created": created})
}

func cmdReindex(args []string) int {
	fs := flag.NewFlagSet("reindex", flag.ContinueOnError)
	fs.Bool("full", false, "full rebuild (default and only mode for now)")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		return fail("%v", err)
	}
	if err := store.Reset(cfg.DBPath); err != nil {
		return fail("reset index db: %v", err)
	}
	s, err := store.Open(cfg.DBPath)
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	start := time.Now()
	rep, err := index.New(cfg, s).Full()
	if err != nil {
		return fail("reindex: %v", err)
	}
	return emit(map[string]any{
		"indexed": rep.Indexed,
		"deleted": rep.Deleted,
		"links":   rep.Links,
		"took_ms": time.Since(start).Milliseconds(),
	})
}

// cmdDoctor reports vault/sidecar divergence (missing or orphan sidecars, stray conflict copies,
// duplicate titles) without touching any file. Finding issues is not an error, so it still exits 0;
// callers branch on the issues array, reserving the {"error":...}/exit 1 contract for real failures.
//
// With --fix it repairs the divergence by auto-numbered restore (see doctor.Fix), then rebuilds the
// index so the cache reflects the repaired vault.
func cmdDoctor(args []string) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fix := fs.Bool("fix", false, "repair divergence by auto-numbered restore, then reindex")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		return fail("%v", err)
	}

	if *fix {
		startID := time.Now().Unix() * 1000
		rep, err := doctor.Fix(cfg, startID)
		if err != nil {
			return fail("doctor --fix: %v", err)
		}
		out := map[string]any{
			"changed": rep.Changed,
			"fixed":   rep.Fixed,
			"skipped": rep.Skipped,
		}
		if rep.Changed {
			if err := store.Reset(cfg.DBPath); err != nil {
				return fail("reset index db: %v", err)
			}
			s, err := store.Open(cfg.DBPath)
			if err != nil {
				return fail("%v", err)
			}
			defer s.Close()
			ix, err := index.New(cfg, s).Full()
			if err != nil {
				return fail("reindex: %v", err)
			}
			out["reindexed"] = ix.Indexed
		}
		return emit(out)
	}

	rep, err := doctor.Diagnose(cfg)
	if err != nil {
		return fail("doctor: %v", err)
	}
	return emit(map[string]any{
		"scanned": rep.Scanned,
		"issues":  rep.Issues,
		"ok":      len(rep.Issues) == 0,
	})
}

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
		def, err := defaultTemplateSpec(cfg, config.KindNote)
		if err != nil {
			return nil, fmt.Errorf("resolve default template: %v", err)
		}
		effectiveTemplate = def
	}
	if effectiveTemplate != "" {
		rendered, err := renderTemplate(cfg, effectiveTemplate, title, noteID, config.KindNote, parent, time.Now())
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

	day := startOfDay(time.Now()).AddDate(0, 0, *offset)
	name := day.Format(cfg.JournalDateFormat)
	noteID, err := note.IDFromName(name)
	if err != nil {
		return fail("journal id: %v", err)
	}
	date := day.Format(cfg.DateFormat)
	path := cfg.JournalPath(name)

	created := false
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.MkdirAll(cfg.JournalDir(), 0o755); err != nil {
			return fail("create vault dir: %v", err)
		}
		jbody := ""
		effectiveTemplate := strings.TrimSpace(*template)
		if effectiveTemplate == "" && strings.TrimSpace(body) == "" {
			def, err := defaultTemplateSpec(cfg, config.KindJournal)
			if err != nil {
				return fail("resolve default template: %v", err)
			}
			effectiveTemplate = def
		}
		if effectiveTemplate != "" {
			rendered, err := renderTemplate(cfg, effectiveTemplate, name, noteID, config.KindJournal, "", day)
			if err != nil {
				return fail("render template: %v", err)
			}
			jbody = ensureTrailingNewline(rendered)
		} else if strings.TrimSpace(body) != "" {
			jbody = ensureTrailingNewline(body)
		}
		if err := os.WriteFile(path, []byte(jbody), 0o644); err != nil {
			return fail("write journal: %v", err)
		}
		if err := note.WriteMetadata(
			cfg.MetadataPath(noteID),
			note.Metadata{Title: name, Tags: []string{"journal"}, Created: date},
		); err != nil {
			return fail("write metadata: %v", err)
		}
		if err := index.New(cfg, s).One(path); err != nil {
			return fail("index journal: %v", err)
		}
		created = true
	} else if strings.TrimSpace(body) != "" {
		// The journal exists already; content flags only apply on creation. Point to append so the
		// daily-log workflow has an explicit path rather than silently dropping the body.
		return fail("journal %s already exists; use `track append --id %d` to add content or tags", name, noteID)
	}

	// Roll the day up into month and year summary notes: journal/<yyyyMM>.md links each day, and
	// journal/<yyyy>.md links each month. Both are idempotent (links are appended only when missing),
	// so reopening a journal self-heals the summaries without duplicating entries.
	month := day.Format("200601")
	year := day.Format("2006")
	if err := ensureJournalSummary(cfg, s, month, name, date, "journal-month"); err != nil {
		return fail("journal month summary: %v", err)
	}
	if err := ensureJournalSummary(cfg, s, year, month, date, "journal-year"); err != nil {
		return fail("journal year summary: %v", err)
	}

	return emit(map[string]any{"id": noteID, "path": path, "created": created})
}

// ensureJournalSummary makes sure the summary journal note named `name` exists and lists `childTerm`
// as a "- [[childTerm]]" bullet. The note is created (and indexed) when absent, and the link is
// appended only when missing, so calling it repeatedly is safe. date seeds the created metadata when
// the note is new; kindTag distinguishes month/year rollups from daily notes.
func ensureJournalSummary(cfg *config.Config, s *store.Store, name, childTerm, date, kindTag string) error {
	noteID, err := note.IDFromName(name)
	if err != nil {
		return err
	}
	path := cfg.JournalPath(name)

	body := ""
	exists := true
	if raw, err := os.ReadFile(path); err == nil {
		body = string(raw)
	} else if os.IsNotExist(err) {
		exists = false
	} else {
		return err
	}

	link := "[[" + childTerm + "]]"
	changed := false
	if !exists {
		if err := os.MkdirAll(cfg.JournalDir(), 0o755); err != nil {
			return err
		}
		changed = true
	}
	if !strings.Contains(body, link) {
		if body != "" && !strings.HasSuffix(body, "\n") {
			body += "\n"
		}
		body += "- " + link + "\n"
		changed = true
	}
	if !changed {
		return nil
	}

	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return err
	}
	if !exists {
		if err := note.WriteMetadata(
			cfg.MetadataPath(noteID),
			note.Metadata{Title: name, Tags: []string{"journal", kindTag}, Created: date},
		); err != nil {
			return err
		}
	}
	return index.New(cfg, s).One(path)
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

// tagsFlag collects repeatable --tag values; each value may itself be comma-separated.
type tagsFlag []string

func (t *tagsFlag) String() string { return strings.Join(*t, ",") }

func (t *tagsFlag) Set(v string) error {
	for _, part := range strings.Split(v, ",") {
		if p := strings.TrimSpace(part); p != "" {
			*t = append(*t, p)
		}
	}
	return nil
}

// dedupTags trims and de-duplicates tags, preserving first-seen order. It returns nil for an empty set.
func dedupTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(tags))
	var out []string
	for _, tg := range tags {
		tg = strings.TrimSpace(tg)
		if tg == "" || seen[tg] {
			continue
		}
		seen[tg] = true
		out = append(out, tg)
	}
	return out
}

// readBody returns body text from the --body flag when it was set, otherwise from piped stdin.
// An interactive terminal (no pipe) yields an empty body instead of blocking on a read.
func readBody(fs *flag.FlagSet, flagVal string) (string, error) {
	if flagWasSet(fs, "body") {
		return flagVal, nil
	}
	fi, err := os.Stdin.Stat()
	if err != nil {
		return "", nil
	}
	if fi.Mode()&os.ModeCharDevice != 0 {
		return "", nil
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// flagWasSet reports whether the named flag was explicitly provided on the command line.
func flagWasSet(fs *flag.FlagSet, name string) bool {
	found := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

func cmdKeywords(args []string) int {
	cfg, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	kws, err := s.Keywords()
	if err != nil {
		return fail("keywords: %v", err)
	}
	if kws == nil {
		kws = []store.Keyword{}
	}
	for i := range kws {
		kws[i].Path = cfg.PathForKind(kws[i].FileKind, kws[i].NoteID)
	}
	return emit(map[string]any{"keywords": kws})
}

func cmdResolve(args []string) int {
	fs := flag.NewFlagSet("resolve", flag.ContinueOnError)
	term := fs.String("term", "", "keyword to resolve")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}
	if *term == "" {
		return fail("--term is required")
	}

	cfg, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	ref, found, err := s.ResolveTerm(*term)
	if err != nil {
		return fail("resolve: %v", err)
	}
	if !found {
		return emit(map[string]any{"found": false})
	}
	return emit(map[string]any{"found": true, "note_id": ref.NoteID, "path": cfg.PathForKind(ref.FileKind, ref.NoteID)})
}

func cmdSearch(args []string) int {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	query := fs.String("query", "", "search query; #tag filters tags")
	limit := fs.Int("limit", 50, "max results")
	scope := fs.String("scope", string(store.SearchAll), "search scope: all, title, body")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}
	if *query == "" {
		return fail("--query is required")
	}

	cfg, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	// Self-heal before reading: the index is a cache keyed by (cache_dir, vault), so another editor's
	// CLI, the web server, or an external/cloud sync may have changed notes this process never indexed.
	// A cheap mtime scan reconciles those before search, so results match the files on disk.
	if _, err := index.New(cfg, s).RefreshIfStale(); err != nil {
		return fail("refresh index: %v", err)
	}

	results, err := searchResults(cfg, s, *query, *limit, store.SearchScope(*scope))
	if err != nil {
		return fail("search: %v", err)
	}
	if results == nil {
		results = []store.SearchResult{}
	}
	return emit(map[string]any{"results": results})
}

func cmdBacklinks(args []string) int {
	fs := flag.NewFlagSet("backlinks", flag.ContinueOnError)
	id := fs.Int64("id", 0, "note id")
	path := fs.String("path", "", "note path (alternative to --id)")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}

	cfg, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	noteID := *id
	if noteID == 0 {
		if *path == "" {
			return fail("--id or --path is required")
		}
		parsed, err := note.IDFromPath(*path)
		if err != nil {
			return fail("invalid path: %v", err)
		}
		noteID = parsed
	}

	back, err := s.Backlinks(noteID)
	if err != nil {
		return fail("backlinks: %v", err)
	}
	if back == nil {
		back = []store.NoteRef{}
	}
	for i := range back {
		back[i].Path = cfg.PathForKind(back[i].FileKind, back[i].NoteID)
	}
	return emit(map[string]any{"backlinks": back})
}

func cmdGraph(args []string) int {
	fs := flag.NewFlagSet("graph", flag.ContinueOnError)
	id := fs.Int64("id", 0, "note id")
	path := fs.String("path", "", "note path (alternative to --id)")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}

	cfg, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	noteID := *id
	if noteID == 0 {
		if *path == "" {
			return fail("--id or --path is required")
		}
		parsed, err := note.IDFromPath(*path)
		if err != nil {
			return fail("invalid path: %v", err)
		}
		noteID = parsed
	}

	graph, err := s.LocalGraph(noteID)
	if err != nil {
		return fail("graph: %v", err)
	}
	for i := range graph.Nodes {
		graph.Nodes[i].Path = cfg.PathForKind(graph.Nodes[i].FileKind, graph.Nodes[i].NoteID)
	}
	return emit(map[string]any{"graph": graph})
}

func cmdWeb(args []string) int {
	fs := flag.NewFlagSet("web", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:8765", "listen address")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}

	cfg, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	fmt.Fprintf(os.Stderr, "track web: http://%s\n", *addr)
	if err := webui.Serve(cfg, s, *addr); err != nil {
		return fail("web: %v", err)
	}
	return 0
}

func searchResults(cfg *config.Config, s *store.Store, query string, limit int, scope store.SearchScope) ([]store.SearchResult, error) {
	if limit <= 0 {
		limit = 50
	}
	switch scope {
	case store.SearchTitle:
		results, err := s.SearchScoped(query, limit, scope)
		addSearchPaths(cfg, results)
		return results, err
	case store.SearchAll:
		results, err := s.SearchScoped(query, limit, scope)
		if err != nil {
			return nil, err
		}
		addSearchPaths(cfg, results)
		seen := make(map[int64]bool, len(results))
		for _, result := range results {
			seen[result.NoteID] = true
		}
		body, err := bodySearchResults(cfg, s, query, limit-len(results), seen)
		if err != nil {
			return nil, err
		}
		return append(results, body...), nil
	case store.SearchBody:
		return bodySearchResults(cfg, s, query, limit, nil)
	default:
		return nil, fmt.Errorf("unknown search scope %q", scope)
	}
}

func addSearchPaths(cfg *config.Config, results []store.SearchResult) {
	for i := range results {
		results[i].Path = cfg.PathForKind(results[i].FileKind, results[i].NoteID)
	}
}

func bodySearchResults(cfg *config.Config, s *store.Store, query string, limit int, skip map[int64]bool) ([]store.SearchResult, error) {
	if limit <= 0 {
		return []store.SearchResult{}, nil
	}
	notes, err := s.SearchRefs()
	if err != nil {
		return nil, err
	}
	refs := make(map[int64]store.SearchResult, len(notes))
	for _, n := range notes {
		refs[n.NoteID] = n
	}
	paths, err := scanSearchFiles(cfg)
	if err != nil {
		return nil, err
	}
	var out []store.SearchResult
	for _, path := range paths {
		id, err := note.IDFromPath(path)
		ref, indexed := refs[id]
		if err != nil || !indexed || skip[id] {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		body, _, _ := note.SplitLegacyFootmatter(string(raw))
		line, snippet := bodyLineMatch(body, query)
		if line == 0 {
			continue
		}
		out = append(out, store.SearchResult{
			NoteID:   id,
			FileKind: ref.FileKind,
			Path:     cfg.PathForKind(ref.FileKind, id),
			Title:    ref.Title,
			Tags:     ref.Tags,
			Line:     line,
			Snippet:  snippet,
			Mtime:    ref.Mtime,
		})
	}
	sortSearchResults(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func sortSearchResults(results []store.SearchResult) {
	slices.SortFunc(results, func(a, b store.SearchResult) int {
		if a.Mtime != b.Mtime {
			return cmpDesc(a.Mtime, b.Mtime)
		}
		return cmpDesc(a.NoteID, b.NoteID)
	})
}

func cmpDesc[T ~int64](a, b T) int {
	switch {
	case a > b:
		return -1
	case a < b:
		return 1
	default:
		return 0
	}
}

func scanSearchFiles(cfg *config.Config) ([]string, error) {
	var out []string
	for _, root := range []string{cfg.NoteDir(), cfg.JournalDir()} {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				if d == nil {
					return nil
				}
				return err
			}
			if d.IsDir() {
				if path != root {
					return filepath.SkipDir
				}
				return nil
			}
			if slices.Contains(cfg.Extensions, filepath.Ext(path)) {
				out = append(out, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	slices.Sort(out)
	return out, nil
}

func bodyLineMatch(body, query string) (int, string) {
	lq := strings.ToLower(query)
	for i, line := range strings.Split(body, "\n") {
		if strings.Contains(strings.ToLower(line), lq) {
			return i + 1, truncateSearchSnippet(strings.TrimSpace(line), 120)
		}
	}
	return 0, ""
}

func truncateSearchSnippet(s string, max int) string {
	if len(s) <= max {
		return s
	}
	end := max
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end] + "…"
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

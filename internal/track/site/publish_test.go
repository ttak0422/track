package site

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

// writeVaultNoteMeta writes a note and the full sidecar it publishes from — tags, props, icon, and
// the rest. It is writeVaultNote for the cases where the note's metadata is the thing under test.
func writeVaultNoteMeta(t *testing.T, cfg *config.Config, id int64, body string, meta note.Metadata) {
	t.Helper()
	path := cfg.NotePath(id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if meta.Version == 0 {
		meta.Version = note.MaxMetadataVersion
	}
	if err := note.WriteMetadata(cfg.MetadataPath(id), meta); err != nil {
		t.Fatal(err)
	}
}

// buildAll indexes the vault and publishes every note in it, returning the output directory.
func buildAll(t *testing.T, cfg *config.Config, s *store.Store, root int64, ids ...int64) string {
	t.Helper()
	if _, err := index.New(cfg, s).Full(); err != nil {
		t.Fatalf("index: %v", err)
	}
	out := t.TempDir()
	if _, err := Build(cfg, s, Options{Root: root, IDs: ids}, fakeFrontend(t), out); err != nil {
		t.Fatalf("Build: %v", err)
	}
	return out
}

// The bundle derives breadcrumb data from each note's "up" relation property, resolved by title
// within the published set. A parent outside the set is skipped, like any other out-of-set link.
func TestBuildPublishesHierarchy(t *testing.T) {
	cfg, s := vaultStore(t)
	writeVaultNoteMeta(t, cfg, 100, "# Welcome\n\nsee [[Topic]]\n", note.Metadata{Title: "Welcome"})
	writeVaultNoteMeta(t, cfg, 200, "# Topic\n", note.Metadata{Title: "Topic", Props: map[string]any{"up": "[[Welcome]]"}})
	writeVaultNoteMeta(t, cfg, 300, "# Deep\n", note.Metadata{Title: "Deep", Props: map[string]any{"up": "[[Topic]]"}})
	writeVaultNoteMeta(t, cfg, 400, "# Outside\n", note.Metadata{Title: "Outside", Props: map[string]any{"up": "[[Unpublished]]"}})

	out := buildAll(t, cfg, s, 100, 200, 300, 400)
	noteOf := func(id int64) jsonNoteResponse {
		t.Helper()
		return readJSON[jsonNoteResponse](t, filepath.Join(out, "data", "note", PublishID(id)+".json"))
	}

	deep := noteOf(300)
	if len(deep.Trail) != 2 || deep.Trail[0].Title != "Welcome" || deep.Trail[1].Title != "Topic" {
		t.Fatalf("deep trail = %+v, want Welcome then Topic", deep.Trail)
	}

	topic := noteOf(200)
	if len(topic.Trail) != 1 || topic.Trail[0].Title != "Welcome" {
		t.Fatalf("topic trail = %+v, want Welcome", topic.Trail)
	}
	if len(topic.Children) != 1 || topic.Children[0].Title != "Deep" {
		t.Fatalf("topic children = %+v, want Deep", topic.Children)
	}

	root := noteOf(100)
	if len(root.Trail) != 0 {
		t.Fatalf("root trail should be empty, got %+v", root.Trail)
	}
	if len(root.Children) != 1 || root.Children[0].Title != "Topic" {
		t.Fatalf("root children = %+v, want Topic only", root.Children)
	}

	// An up pointing outside the published set leaves the note with no trail rather than a dangling one.
	outside := noteOf(400)
	if len(outside.Trail) != 0 {
		t.Fatalf("an out-of-set parent should leave no trail, got %+v", outside.Trail)
	}

	// The same relation, whole: hierarchy.json carries the forest the rail's menu draws, prebuilt so
	// the browser never walks it. Only notes the hierarchy places are in it — "Outside" resolves to
	// nothing published, so it is absent rather than standing as a second root.
	tree := readJSON[struct {
		Hierarchy []jsonHierarchyNode `json:"hierarchy"`
	}](t, filepath.Join(out, "data", "hierarchy.json"))
	if len(tree.Hierarchy) != 1 || tree.Hierarchy[0].Title != "Welcome" {
		t.Fatalf("hierarchy roots = %+v, want Welcome alone", tree.Hierarchy)
	}
	kids := tree.Hierarchy[0].Children
	if len(kids) != 1 || kids[0].Title != "Topic" || kids[0].NoteID != PublishID(200) {
		t.Fatalf("Welcome's children = %+v, want the published Topic", kids)
	}
	if deep := kids[0].Children; len(deep) != 1 || deep[0].Title != "Deep" {
		t.Fatalf("Topic's children = %+v, want Deep", deep)
	}
}

// Tags drive three published surfaces at once: the note list, the ```track-query blocks expanded at
// build time, and a real page per used tag and each of its ancestors.
func TestBuildPublishesTagsAndQueryBlocks(t *testing.T) {
	cfg, s := vaultStore(t)
	writeVaultNoteMeta(t, cfg, 100, "# Home\n\n```track-query\nTABLE title, tags FROM #docs SORT title\n```\n",
		note.Metadata{Title: "Home", Tags: []string{"docs"}})
	writeVaultNoteMeta(t, cfg, 200, "# Alpha\n", note.Metadata{Title: "Alpha", Tags: []string{"docs/guide"}})
	writeVaultNoteMeta(t, cfg, 300, "# Beta\n\nno tags here\n", note.Metadata{Title: "Beta"})

	out := buildAll(t, cfg, s, 100, 200, 300)

	notes := readJSON[struct {
		Notes []jsonSearchResult `json:"notes"`
	}](t, filepath.Join(out, "data", "notes.json"))
	tagsByTitle := map[string][]string{}
	for _, n := range notes.Notes {
		tagsByTitle[n.Title] = n.Tags
	}
	if len(tagsByTitle["Alpha"]) != 1 || tagsByTitle["Alpha"][0] != "docs/guide" {
		t.Fatalf("Alpha tags = %v", tagsByTitle["Alpha"])
	}

	// The fence is expanded to a Markdown result table at build time; #docs matches the nested
	// docs/guide tag too, and an untagged note matches neither.
	root := readJSON[jsonNoteResponse](t, filepath.Join(out, "data", "note", PublishID(100)+".json"))
	if strings.Contains(root.Note.Body, "```track-query") {
		t.Fatalf("query fence should be expanded: %q", root.Note.Body)
	}
	if !strings.Contains(root.Note.Body, "| [[Alpha]] | docs/guide |") ||
		!strings.Contains(root.Note.Body, "| [[Home]] | docs |") {
		t.Fatalf("expanded table missing rows: %q", root.Note.Body)
	}
	if strings.Contains(root.Note.Body, "[[Beta]]") {
		t.Fatalf("untagged note must not match #docs: %q", root.Note.Body)
	}

	for _, rel := range []string{"tags/docs/index.html", "tags/docs/guide/index.html"} {
		if !fileExists(filepath.Join(out, rel)) {
			t.Fatalf("missing tag page %s", rel)
		}
	}
}

// Icons resolve through config.NoteIcon's one precedence rule, the same one the live workspace
// applies: the note's own sidecar icon, then the vault config's tag mapping, then its kind mapping.
func TestBuildPublishesIcons(t *testing.T) {
	cfg, s := vaultStore(t)
	cfg.Icons = config.IconMap{Tags: map[string]string{"guide": "📚"}, Kinds: map[string]string{"note": "📄"}}
	writeVaultNoteMeta(t, cfg, 100, "# Own\n", note.Metadata{Title: "Own", Icon: "💡", Tags: []string{"guide"}})
	writeVaultNoteMeta(t, cfg, 200, "# Tagged\n", note.Metadata{Title: "Tagged", Tags: []string{"guide"}})
	writeVaultNoteMeta(t, cfg, 300, "# Plain\n", note.Metadata{Title: "Plain"})

	out := buildAll(t, cfg, s, 100, 200, 300)

	list := readJSON[struct {
		Notes []jsonSearchResult `json:"notes"`
	}](t, filepath.Join(out, "data", "notes.json"))
	icons := map[string]string{}
	for _, n := range list.Notes {
		icons[n.Title] = n.Icon
	}
	// Own carries the guide tag too: its own icon wins over the mapping the tag would supply.
	for title, want := range map[string]string{"Own": "💡", "Tagged": "📚", "Plain": "📄"} {
		if icons[title] != want {
			t.Errorf("%s icon = %q, want %q", title, icons[title], want)
		}
	}
}

// A saved query named in the vault config is runnable from a fence by name, so a page can carry the
// query the vault already has rather than repeating its expression.
func TestBuildExpandsSavedQueryFence(t *testing.T) {
	cfg, s := vaultStore(t)
	cfg.Queries = map[string]string{"guides": "TABLE title FROM #docs SORT title"}
	writeVaultNoteMeta(t, cfg, 100, "# Home\n\n```track-query\nsaved: guides\n```\n", note.Metadata{Title: "Home"})
	writeVaultNoteMeta(t, cfg, 200, "# Alpha\n", note.Metadata{Title: "Alpha", Tags: []string{"docs"}})

	out := buildAll(t, cfg, s, 100, 200)

	root := readJSON[jsonNoteResponse](t, filepath.Join(out, "data", "note", PublishID(100)+".json"))
	if !strings.Contains(root.Note.Body, "| [[Alpha]] |") {
		t.Fatalf("saved query should have expanded to its result table: %q", root.Note.Body)
	}
}

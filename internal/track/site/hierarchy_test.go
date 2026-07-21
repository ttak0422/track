package site

import (
	"os"
	"path/filepath"
	"testing"
)

// The bundle derives breadcrumb data from each doc's up-targets, resolved within the published set.
// In dir mode those come from the site config's pages map — the directory's stand-in for a vault
// note's sidecar — never from an inline "up::" field, which stays a plain prose property (ADR
// 0032/0049). This is exactly how the help site gets its hierarchy.
func TestBuildDirPublishesHierarchy(t *testing.T) {
	src := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(src, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("index.md", "# Welcome\n\nsee [[Topic]]\n")
	write("topic.md", "# Topic\n")
	write("deep.md", "# Deep\n")
	write("stray.md", "# Stray\n\nup:: [[Welcome]]\n")
	// topic's parent by file base name, deep's by page title: both keys a wiki link resolves by.
	write("site.yml", "pages:\n  topic: {up: index}\n  deep: {up: Topic}\n")

	out := t.TempDir()
	if _, err := BuildDir(src, "", fakeFrontend(t), out); err != nil {
		t.Fatalf("BuildDir: %v", err)
	}

	resolve := readJSON[map[string]jsonRef](t, filepath.Join(out, "data", "resolve.json"))
	noteOf := func(key string) jsonNoteResponse {
		t.Helper()
		return readJSON[jsonNoteResponse](t, filepath.Join(out, "data", "note", resolve[key].NoteID+".json"))
	}

	deep := noteOf("Deep")
	if len(deep.Trail) != 2 || deep.Trail[0].Title != "Welcome" || deep.Trail[1].Title != "Topic" {
		t.Fatalf("deep trail = %+v, want Welcome then Topic", deep.Trail)
	}

	topic := noteOf("Topic")
	if len(topic.Trail) != 1 || topic.Trail[0].Title != "Welcome" {
		t.Fatalf("topic trail = %+v, want Welcome", topic.Trail)
	}
	if len(topic.Children) != 1 || topic.Children[0].Title != "Deep" {
		t.Fatalf("topic children = %+v, want Deep", topic.Children)
	}

	root := noteOf("Welcome")
	if len(root.Trail) != 0 {
		t.Fatalf("root trail should be empty, got %+v", root.Trail)
	}
	if len(root.Children) != 1 || root.Children[0].Title != "Topic" {
		t.Fatalf("root children = %+v, want Topic only", root.Children)
	}

	// An inline "up::" field in a directory page is prose, not hierarchy: no trail for the page,
	// no child slot beside the target (root's children above already exclude it).
	stray := noteOf("Stray")
	if len(stray.Trail) != 0 || len(stray.Children) != 0 {
		t.Fatalf("stray should have no hierarchy, got %+v / %+v", stray.Trail, stray.Children)
	}
}

// A page's H1 may spell another page's file base name. up resolves the way home does — base name
// first, never through the merged link map — so the shadowing title must not steal the parent.
func TestBuildDirUpResolvesBaseNameFirst(t *testing.T) {
	src := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(src, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("child.md", "# Child\n")
	write("guide.md", "# Guide\n")
	write("zeta.md", "# guide\n") // title shadows guide.md's base name in the link map
	write("site.yml", "home: guide\npages:\n  child: {up: guide}\n")

	out := t.TempDir()
	if _, err := BuildDir(src, "", fakeFrontend(t), out); err != nil {
		t.Fatalf("BuildDir: %v", err)
	}

	resolve := readJSON[map[string]jsonRef](t, filepath.Join(out, "data", "resolve.json"))
	child := readJSON[jsonNoteResponse](t, filepath.Join(out, "data", "note", resolve["Child"].NoteID+".json"))
	if len(child.Trail) != 1 || child.Trail[0].Title != "Guide" {
		t.Fatalf("child trail = %+v, want guide.md's Guide, not the shadowing title", child.Trail)
	}
}

package site

import (
	"os"
	"path/filepath"
	"testing"
)

// The bundle derives breadcrumb data from each doc's "up" relation property, resolved within the
// published set — the dir mode exercised here is exactly how the help site gets its hierarchy.
func TestBuildDirPublishesHierarchy(t *testing.T) {
	src := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(src, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("index.md", "# Welcome\n\nsee [[Topic]]\n")
	write("topic.md", "# Topic\n\nup:: [[Welcome]]\n")
	write("deep.md", "# Deep\n\nup:: [[Topic]]\n")
	write("stray.md", "# Stray\n\nup:: [[Nowhere]]\n")

	out := t.TempDir()
	if _, err := BuildDir(src, "index", "", fakeFrontend(t), out); err != nil {
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
		t.Fatalf("root children = %+v, want Topic", root.Children)
	}

	// An up-link that resolves to nothing in the set is not a trail entry.
	stray := noteOf("Stray")
	if len(stray.Trail) != 0 || len(stray.Children) != 0 {
		t.Fatalf("stray should have no hierarchy, got %+v / %+v", stray.Trail, stray.Children)
	}
}

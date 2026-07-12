package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readNote returns the on-disk body of the note whose path a command reported.
func readNote(t *testing.T, decoded map[string]any, key string) string {
	t.Helper()
	p, ok := decoded[key].(string)
	if !ok {
		t.Fatalf("no %q path in %v", key, decoded)
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(raw)
}

func TestCaptureToInbox(t *testing.T) {
	vault := t.TempDir()
	// No inbox note exists yet: the first capture creates the configured "Inbox".
	out, code := runIn(t, vault, "capture", "--body", "buy milk")
	if code != 0 {
		t.Fatalf("capture failed: %v", out)
	}
	if out["target"] != "Inbox" {
		t.Fatalf("target: %v", out["target"])
	}
	body := readNote(t, out, "path")
	if !strings.Contains(body, "buy milk") {
		t.Fatalf("inbox missing capture:\n%s", body)
	}

	// A second capture packs onto the first (both are list-free lines separated by a blank line here).
	out2, code := runIn(t, vault, "capture", "--body", "call bob")
	if code != 0 {
		t.Fatalf("second capture failed: %v", out2)
	}
	body = readNote(t, out2, "path")
	if !strings.Contains(body, "buy milk") || !strings.Contains(body, "call bob") {
		t.Fatalf("inbox lost an entry:\n%s", body)
	}
}

func TestCaptureUnderHeadingWithTemplate(t *testing.T) {
	vault := t.TempDir()
	if _, code := runIn(t, vault, "new", "--title", "Inbox", "--body", "# Inbox\n\n## Tasks\n- [ ] existing\n"); code != 0 {
		t.Fatal("setup new failed")
	}
	// A capture template: the captured text fills {{ title }}.
	tpl := filepath.Join(t.TempDir(), "1.template.md")
	if err := os.WriteFile(tpl, []byte("<!-- track-template\nname: task\n-->\n- [ ] {{ title }}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := runIn(t, vault, "capture", "--target", "Inbox#Tasks", "--template", tpl, "--body", "write tests")
	if code != 0 {
		t.Fatalf("capture failed: %v", out)
	}
	body := readNote(t, out, "path")
	want := "## Tasks\n- [ ] existing\n- [ ] write tests\n"
	if !strings.Contains(body, want) {
		t.Fatalf("templated entry did not pack under Tasks:\n%s", body)
	}
}

func TestCaptureAmbiguousHeadingRefused(t *testing.T) {
	vault := t.TempDir()
	if _, code := runIn(t, vault, "new", "--title", "Note", "--body", "## Tasks\n- a\n\n## Tasks\n- b\n"); code != 0 {
		t.Fatal("setup failed")
	}
	out, code := runIn(t, vault, "capture", "--target", "Note#Tasks", "--body", "x")
	if code == 0 {
		t.Fatalf("expected refusal, got %v", out)
	}
	if !strings.Contains(out["error"].(string), "ambiguous") {
		t.Fatalf("error: %v", out["error"])
	}
}

func TestCaptureExplicitMissingTargetRefused(t *testing.T) {
	vault := t.TempDir()
	out, code := runIn(t, vault, "capture", "--target", "Nope#H", "--body", "x")
	if code == 0 {
		t.Fatalf("expected error for missing explicit target, got %v", out)
	}
}

func TestRefileCrossNoteKeepsLinksResolving(t *testing.T) {
	vault := t.TempDir()
	if _, code := runIn(t, vault, "new", "--title", "Target", "--body", "# Target\n"); code != 0 {
		t.Fatal("setup Target failed")
	}
	if _, code := runIn(t, vault, "new", "--title", "Source",
		"--body", "# Source\n\n## Move\ntext [[Target]]\n\n### Child\nc\n\n## Keep\nk\n"); code != 0 {
		t.Fatal("setup Source failed")
	}
	if _, code := runIn(t, vault, "new", "--title", "Dest", "--body", "# Dest\n\n## Bucket\n- one\n"); code != 0 {
		t.Fatal("setup Dest failed")
	}

	out, code := runIn(t, vault, "refile", "--from", "Source#Move", "--to", "Dest#Bucket")
	if code != 0 {
		t.Fatalf("refile failed: %v", out)
	}

	dest := readNote(t, map[string]any{"p": destPath(t, vault, "Dest")}, "p")
	for _, want := range []string{"## Move", "text [[Target]]", "### Child"} {
		if !strings.Contains(dest, want) {
			t.Fatalf("destination missing %q:\n%s", want, dest)
		}
	}
	src := readNote(t, map[string]any{"p": destPath(t, vault, "Source")}, "p")
	if strings.Contains(src, "## Move") || strings.Contains(src, "[[Target]]") {
		t.Fatalf("source still holds the moved subtree:\n%s", src)
	}
	if !strings.Contains(src, "## Keep") {
		t.Fatalf("source lost unrelated content:\n%s", src)
	}

	// The moved link now resolves from Dest, not Source: the index followed the text.
	bl, code := runIn(t, vault, "backlinks", "--path", destPath(t, vault, "Target"))
	if code != 0 {
		t.Fatalf("backlinks failed: %v", bl)
	}
	titles := backlinkTitles(bl)
	if !titles["Dest"] || titles["Source"] {
		t.Fatalf("backlinks did not follow the move: %v", titles)
	}
}

func TestRefileSameNote(t *testing.T) {
	vault := t.TempDir()
	if _, code := runIn(t, vault, "new", "--title", "N",
		"--body", "## A\n- item\n\n## B\ntext\n"); code != 0 {
		t.Fatal("setup failed")
	}
	out, code := runIn(t, vault, "refile", "--from", "N#A", "--to", "N#B")
	if code != 0 {
		t.Fatalf("refile failed: %v", out)
	}
	if out["same_note"] != true {
		t.Fatalf("expected same_note: %v", out)
	}
	body := readNote(t, map[string]any{"p": destPath(t, vault, "N")}, "p")
	// A moved under B; A's heading is gone from the top.
	if !strings.Contains(body, "## B\ntext\n\n## A\n- item\n") {
		t.Fatalf("same-note move wrong:\n%s", body)
	}
}

func TestRefileListItem(t *testing.T) {
	vault := t.TempDir()
	if _, code := runIn(t, vault, "new", "--title", "Src",
		"--body", "## In\n- keep\n- move [[X]]\n  - sub\n- tail\n"); code != 0 {
		t.Fatal("setup Src failed")
	}
	if _, code := runIn(t, vault, "new", "--title", "Dst", "--body", "## Done\n- prior\n"); code != 0 {
		t.Fatal("setup Dst failed")
	}
	// Line 3 is "- move [[X]]"; its nested "  - sub" travels with it.
	out, code := runIn(t, vault, "refile", "--from", "Src", "--line", "3", "--to", "Dst#Done")
	if code != 0 {
		t.Fatalf("refile --line failed: %v", out)
	}
	dst := readNote(t, map[string]any{"p": destPath(t, vault, "Dst")}, "p")
	if !strings.Contains(dst, "- prior\n- move [[X]]\n  - sub\n") {
		t.Fatalf("list item did not move with its nesting:\n%s", dst)
	}
	src := readNote(t, map[string]any{"p": destPath(t, vault, "Src")}, "p")
	if strings.Contains(src, "move [[X]]") {
		t.Fatalf("source still holds the item:\n%s", src)
	}
	if !strings.Contains(src, "- keep") || !strings.Contains(src, "- tail") {
		t.Fatalf("source lost sibling items:\n%s", src)
	}
}

func TestArchiveRecordsProvenance(t *testing.T) {
	vault := t.TempDir()
	t.Setenv("TRACK_ARCHIVE_NOTE", "Archive")
	if _, code := runIn(t, vault, "new", "--title", "Notes",
		"--body", "# Notes\n\n## Old idea\nstuff [[Ref]]\n\n## Live\nkeep\n"); code != 0 {
		t.Fatal("setup failed")
	}
	out, code := runIn(t, vault, "archive", "Notes#Old idea")
	if code != 0 {
		t.Fatalf("archive failed: %v", out)
	}
	if out["archive"] != "Archive" {
		t.Fatalf("archive title: %v", out["archive"])
	}
	arch := readNote(t, out, "archive_path")
	for _, want := range []string{"## Old idea", "Archived from [[Notes]] on", "stuff [[Ref]]"} {
		if !strings.Contains(arch, want) {
			t.Fatalf("archive missing %q:\n%s", want, arch)
		}
	}
	src := readNote(t, map[string]any{"p": destPath(t, vault, "Notes")}, "p")
	if strings.Contains(src, "## Old idea") {
		t.Fatalf("source still holds the archived section:\n%s", src)
	}
	if !strings.Contains(src, "## Live") {
		t.Fatalf("source lost live content:\n%s", src)
	}
}

// destPath resolves a note title to its file path via the CLI's own resolver.
func destPath(t *testing.T, vault, title string) string {
	t.Helper()
	out, code := runIn(t, vault, "resolve", "--term", title)
	if code != 0 {
		t.Fatalf("resolve %q failed: %v", title, out)
	}
	p, ok := out["path"].(string)
	if !ok {
		t.Fatalf("resolve %q gave no path: %v", title, out)
	}
	return p
}

func backlinkTitles(decoded map[string]any) map[string]bool {
	out := map[string]bool{}
	list, _ := decoded["backlinks"].([]any)
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			if title, ok := m["title"].(string); ok {
				out[title] = true
			}
		}
	}
	return out
}

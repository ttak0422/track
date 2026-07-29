package babel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTanglePlanGroupsInNoteOrder(t *testing.T) {
	body := strings.Join([]string{
		"```sh :tangle build.sh",
		"echo first",
		"```",
		"```lua",
		"print('not tangled')",
		"```",
		"```sh :tangle other.sh",
		"echo elsewhere",
		"```",
		"```sh :tangle build.sh",
		"echo second",
		"```",
	}, "\n")

	plan, err := TanglePlan(ParseBlocks(body))
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan) != 2 {
		t.Fatalf("expected 2 targets, got %+v", plan)
	}
	if plan[0].Path != "build.sh" || plan[1].Path != "other.sh" {
		t.Fatalf("target order: %+v", plan)
	}
	if plan[0].Blocks != 2 || plan[0].Content != "echo first\n\necho second\n" {
		t.Fatalf("build.sh content: %+v", plan[0])
	}
	if plan[1].Blocks != 1 || plan[1].Content != "echo elsewhere\n" {
		t.Fatalf("other.sh content: %+v", plan[1])
	}
}

func TestTanglePlanSkipsNoAndRejectsBareYes(t *testing.T) {
	plan, err := TanglePlan(ParseBlocks("```sh :tangle no\necho x\n```"))
	if err != nil || len(plan) != 0 {
		t.Fatalf(":tangle no should be skipped, got %+v, %v", plan, err)
	}
	_, err = TanglePlan(ParseBlocks("```sh :name b :tangle yes\necho x\n```"))
	if err == nil || !strings.Contains(err.Error(), "explicit file name") {
		t.Fatalf(":tangle yes should be rejected, got %v", err)
	}
}

func TestTanglePlanExpandsNoweb(t *testing.T) {
	body := strings.Join([]string{
		"```sh :name lib",
		"echo lib",
		"```",
		"```sh :tangle out.sh :noweb tangle",
		"<<lib>>",
		"echo main",
		"```",
		"```sh :tangle raw.sh",
		"<<lib>>",
		"```",
	}, "\n")

	plan, err := TanglePlan(ParseBlocks(body))
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan[0].Content != "echo lib\necho main\n" {
		t.Fatalf(":noweb tangle should expand, got %q", plan[0].Content)
	}
	// No :noweb -> the reference is written out literally.
	if plan[1].Content != "<<lib>>\n" {
		t.Fatalf("default :noweb no should not expand, got %q", plan[1].Content)
	}
}

func TestTanglePlanSurfacesNowebErrors(t *testing.T) {
	body := "```sh :name a :tangle out.sh :noweb yes\n<<a>>\n```"
	if _, err := TanglePlan(ParseBlocks(body)); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("want cycle error from tangle, got %v", err)
	}
}

func TestResolveTanglePath(t *testing.T) {
	vault := "/vault"
	noteDir := "/vault/note"

	if got, err := ResolveTanglePath(noteDir, vault, "scripts/build.sh"); err != nil || got != "/vault/note/scripts/build.sh" {
		t.Fatalf("relative target: %q, %v", got, err)
	}
	if got, err := ResolveTanglePath(noteDir, vault, "../shared.sh"); err != nil || got != "/vault/shared.sh" {
		t.Fatalf("vault-internal ..: %q, %v", got, err)
	}
	for _, target := range []string{"../../etc/passwd", "/etc/passwd", "../..", ".."} {
		if got, err := ResolveTanglePath(noteDir, vault, target); err == nil {
			t.Fatalf("target %q should be refused, got %q", target, got)
		}
	}
	// The vault directory itself is not a writable file target.
	if _, err := ResolveTanglePath(noteDir, vault, "/vault"); err == nil {
		t.Fatalf("vault root should be refused as a file target")
	}

	// track's own files are off limits: everything under .track/, and the direct children of the
	// flat note/, journal/, and template/ directories. Subdirectories there are user territory.
	for _, target := range []string{
		"/vault/.track/config.yml",
		"/vault/.track/notes/1.yaml",
		"1234.md", // resolves to /vault/note/1234.md — a note file
		"/vault/journal/20260729.md",
		"/vault/template/daily.md",
	} {
		if got, err := ResolveTanglePath(noteDir, vault, target); err == nil {
			t.Fatalf("target %q should be refused, got %q", target, got)
		}
	}
	if _, err := ResolveTanglePath(noteDir, vault, "/vault/journal/scripts/x.sh"); err != nil {
		t.Fatalf("subdirectory under journal/ should be allowed: %v", err)
	}
}

func TestResolveTanglePathSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	vault := filepath.Join(root, "vault")
	noteDir := filepath.Join(vault, "note")
	outside := filepath.Join(root, "outside")
	for _, dir := range []string{noteDir, outside} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(vault, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "file.sh"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "file.sh"), filepath.Join(vault, "evil.sh")); err != nil {
		t.Fatal(err)
	}

	// A directory symlink and a file symlink both point out of the vault: refused even though the
	// lexical path sits inside it.
	for _, target := range []string{"../link/build.sh", "../link/deep/build.sh", "../evil.sh"} {
		if got, err := ResolveTanglePath(noteDir, vault, target); err == nil {
			t.Fatalf("target %q should be refused, got %q", target, got)
		}
	}
	// An ordinary vault path still resolves, existing or not.
	if _, err := ResolveTanglePath(noteDir, vault, "../scripts/build.sh"); err != nil {
		t.Fatalf("plain vault target: %v", err)
	}
}

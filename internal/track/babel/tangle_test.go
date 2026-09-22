package babel

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestTanglePlanLastBlockWinsInNoteOrder(t *testing.T) {
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
	if plan[0].Blocks != 2 || plan[0].Content != "echo second\n" {
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

func TestPlanTangleCanonicalAliasesAndProvenance(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "actual"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("actual", filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	body := "```sh :name first :tangle actual/out.sh\necho first\n```\n" +
		"```sh :tangle other.sh\necho other\n```\n" +
		"```sh :name middle :tangle unused/../actual/out.sh\necho middle\n```\n" +
		"```sh :name last :tangle alias/out.sh\necho last\n\n```"
	path := filepath.Join(root, "input.md")
	plan, err := PlanTangle([]TangleSource{{Path: path, Blocks: ParseBlocks(body)}}, func(_, target string) (string, error) {
		return ResolveTanglePath(root, root, target)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 2 || plan[0].Content != "echo last\n" || plan[0].Blocks != 3 || filepath.Base(plan[1].Path) != "other.sh" {
		t.Fatalf("last-wins plan: %+v", plan)
	}
	want := TangleLocation{Path: path, Line: 10, Block: 3, Name: "last"}
	if plan[0].Source != want {
		t.Fatalf("winning location: got %+v, want %+v", plan[0].Source, want)
	}
	wantOverridden := []TangleLocation{
		{Path: path, Line: 1, Block: 0, Name: "first"},
		{Path: path, Line: 7, Block: 2, Name: "middle"},
	}
	if !reflect.DeepEqual(plan[0].Overridden, wantOverridden) {
		t.Fatalf("overridden locations: %+v", plan[0].Overridden)
	}
}

func TestPlanTangleRejectsCrossSourceAndAncestorConflicts(t *testing.T) {
	resolve := func(_, target string) (string, error) { return filepath.Clean(target), nil }
	for _, pair := range [][2]string{{"out", "sub/../out"}, {"out", "out/child"}} {
		sources := []TangleSource{
			{Path: "/first.md", Blocks: ParseBlocks("```sh :tangle " + pair[0] + "\nfirst\n```")},
			{Path: "/second.md", Blocks: ParseBlocks("text\n```sh :tangle " + pair[1] + "\nsecond\n```")},
		}
		for range 2 {
			plan, err := PlanTangle(sources, resolve)
			if err == nil || plan != nil || !strings.Contains(err.Error(), "/first.md:1") || !strings.Contains(err.Error(), "/second.md:2") {
				t.Fatalf("conflict must reject entire plan with both locations: %+v, %v", plan, err)
			}
			sources[0], sources[1] = sources[1], sources[0]
		}
	}
	blocks := ParseBlocks("```sh :tangle out/child\nx\n```\n```sh :tangle out\ny\n```")
	if _, err := TanglePlan(blocks); err == nil || !strings.Contains(err.Error(), "parent") {
		t.Fatalf("same-source ancestor conflict must fail: %v", err)
	}
}

func TestPlanTangleKeepsNowebLocalAndTextOnly(t *testing.T) {
	resolve := func(_, target string) (string, error) { return target, nil }
	sources := []TangleSource{
		{Path: "/first.md", Blocks: ParseBlocks("```sh :name lib :eval no\nexit 99\n```\n```sh :tangle first.sh :noweb tangle\n<<lib>>\n```")},
		{Path: "/second.md", Blocks: ParseBlocks("```sh :name lib\necho second\n```\n```sh :tangle second.sh :noweb tangle\n<<lib>>\n```")},
	}
	plan, err := PlanTangle(sources, resolve)
	if err != nil || len(plan) != 2 || plan[0].Content != "exit 99\n" || plan[1].Content != "echo second\n" {
		t.Fatalf("local source expansion: %+v, %v", plan, err)
	}
	sources[1].Blocks = ParseBlocks("```sh :tangle second.sh :noweb tangle\n<<lib>>\n```")
	if _, err := PlanTangle(sources, resolve); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("must not resolve another source's block: %v", err)
	}
	sources[0].Blocks = ParseBlocks("```sh :name lib\necho lib\n```\n```sh :tangle out.sh :noweb yes\n<<lib()>>\n```")
	if _, err := PlanTangle(sources[:1], resolve); err == nil || !strings.Contains(err.Error(), "lib()") {
		t.Fatalf("evaluated noweb calls must not run: %v", err)
	}
	sources[0].Blocks = ParseBlocks("```sh :name repeated\na\n```\n```sh :name repeated\nb\n```")
	if _, err := PlanTangle(sources[:1], resolve); err == nil || !strings.Contains(err.Error(), "duplicate babel block name") {
		t.Fatalf("each source must be validated: %v", err)
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

func TestResolveTanglePathProtectsResolvedManagedPaths(t *testing.T) {
	vault := t.TempDir()
	for _, dir := range []string{".track/notes", "note/scripts", "journal", "template", "state"} {
		if err := os.MkdirAll(filepath.Join(vault, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{".track", "note", "journal", "template"} {
		if err := os.Symlink(filepath.Join(vault, name), filepath.Join(vault, "alias-"+name)); err != nil {
			t.Fatal(err)
		}
		if _, err := ResolveTanglePath(vault, vault, "alias-"+name+"/new.md"); err == nil {
			t.Fatalf("alias to %s must not permit writing managed files", name)
		}
	}
	if err := os.WriteFile(filepath.Join(vault, "note", "1.md"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(vault, "note", "1.md"), filepath.Join(vault, "alias.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveTanglePath(vault, vault, "alias.md"); err == nil {
		t.Fatal("file symlink to a managed note must be refused")
	}
	// A managed directory may itself be a symlink. Its real path is also protected.
	if err := os.RemoveAll(filepath.Join(vault, ".track")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(vault, "state"), filepath.Join(vault, ".track")); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveTanglePath(vault, vault, "state/deep/config.yml"); err == nil {
		t.Fatal("resolved metadata root must be protected")
	}
	got, err := ResolveTanglePath(vault, vault, "alias-note/scripts/new.sh")
	want, resolveErr := filepath.EvalSymlinks(filepath.Join(vault, "note", "scripts"))
	if resolveErr != nil {
		t.Fatal(resolveErr)
	}
	if err != nil || got != filepath.Join(want, "new.sh") {
		t.Fatalf("safe alias: got %q, %v", got, err)
	}
}

func TestResolveTanglePathRejectsDanglingLinksAndNonFileTargets(t *testing.T) {
	vault := t.TempDir()
	if err := os.Symlink(filepath.Join(t.TempDir(), "missing"), filepath.Join(vault, "dangling")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(vault, "directory"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, "file"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"dangling", "dangling/new.sh", "directory", "file/new.sh"} {
		if _, err := ResolveTanglePath(vault, vault, target); err == nil {
			t.Fatalf("unsafe output %q must be refused", target)
		}
	}
}

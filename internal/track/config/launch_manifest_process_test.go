//go:build linux || darwin

package config

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/vaultfile"
)

// These tests execute only the inactive helper in a fresh test process. There
// is no public CLI/HTTP caller, index open, native admission or personal vault.
const manifestProcessMarker = "GO_MANIFEST_PROCESS_CHILD"
const manifestResultPrefix = "MANIFEST_PROCESS_RESULT:"

type manifestProcessRequest struct {
	Selector         LaunchSelector
	PrivateRoot      string
	ExpectedRevision string
}
type manifestProcessResult struct {
	Manifest ReadOnlyLaunchManifest
	Error    string
	Changed  bool
}

// Atime is intentionally excluded: legitimate reads may update access times.
// Directory mtimes catch creation/removal even when surviving file bytes match.
type manifestSentinel struct {
	Mode       fs.FileMode
	Size       int64
	ModifiedNS int64
	Hash       [32]byte
	Link       string
}

func manifestInventory(t *testing.T, root string) map[string]manifestSentinel {
	t.Helper()
	out := map[string]manifestSentinel{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		row := manifestSentinel{Mode: info.Mode(), Size: info.Size(), ModifiedNS: info.ModTime().UnixNano()}
		switch {
		case info.Mode().IsRegular():
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			row.Hash = sha256.Sum256(raw)
		case info.IsDir():
		case info.Mode()&os.ModeSymlink != 0:
			row.Link, err = os.Readlink(path)
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("unexpected fixture kind: %s", path)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out[rel] = row
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func assertManifestInventory(t *testing.T, root string, before map[string]manifestSentinel) {
	t.Helper()
	after := manifestInventory(t, root)
	if !reflect.DeepEqual(before, after) {
		for path, want := range before {
			if got, ok := after[path]; !ok || got != want {
				t.Errorf("fixture changed %s: want %+v got %+v", path, want, got)
			}
		}
		for path := range after {
			if _, ok := before[path]; !ok {
				t.Errorf("fixture path created: %s", path)
			}
		}
		t.FailNow()
	}
}

type manifestProcessFixture struct{ root, home, cwd, xdg, private, normal, machine, main, work string }

func newManifestProcessFixture(t *testing.T) manifestProcessFixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := manifestProcessFixture{root: root, home: filepath.Join(root, "home"), cwd: filepath.Join(root, "cwd"), xdg: filepath.Join(root, "xdg"), private: filepath.Join(root, "private"), normal: filepath.Join(root, "normal"), machine: filepath.Join(root, "machine.yml"), main: filepath.Join(root, "home", "main"), work: filepath.Join(root, "home", "work")}
	for _, dir := range []string{f.home, f.cwd, f.xdg, f.private, f.normal, f.main, f.work} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, root := range []string{f.main, f.work} {
		for _, dir := range []string{"note", filepath.Join(".track", "notes")} {
			if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
				t.Fatal(err)
			}
		}
		for name, raw := range map[string]string{"note/42.md": "Synthetic sentinel\n", ".track/notes/42.yaml": "version: 1\ntitle: Sentinel\n", ".track/config.yml": "link_format: title\n"} {
			if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	manifestWriteMachine(t, f, f.work, f.normal)
	return f
}
func manifestWriteMachine(t *testing.T, f manifestProcessFixture, work, normal string) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"default_vault": "main", "cache_dir": normal, "vaults": map[string]string{"main": f.main, "work": work}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.machine, raw, 0600); err != nil {
		t.Fatal(err)
	}
}
func manifestChild(t *testing.T, f manifestProcessFixture, request manifestProcessRequest) manifestProcessResult {
	t.Helper()
	before := manifestInventory(t, f.root)
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"-test.run=^TestReadOnlyLaunchManifestProcess$", "-test.count=1"}
	coverageDir := ""
	if testing.CoverMode() != "" {
		// The coverage test runner otherwise creates/removes gocoverdir under
		// TMPDIR during teardown, changing the audited private-root mtime.
		// Redirect only runner output; helper TMPDIR and every sentinel remain
		// unchanged. A sibling t.TempDir is outside the complete fixture tree.
		coverageDir = t.TempDir()
		args = append(args, "-test.gocoverdir="+coverageDir)
	}
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Dir = f.cwd
	// Each worker sees one explicit context, never an inherited registry/default.
	cmd.Env = []string{"PATH=/usr/bin:/bin", manifestProcessMarker + "=1", "HOME=" + f.home, "XDG_CONFIG_HOME=" + f.home, "XDG_CACHE_HOME=" + f.xdg, "TMPDIR=" + f.private, "TRACK_CONFIG=" + f.machine}
	cmd.Stdin = bytes.NewReader(raw)
	// The test owns this fresh process group. Cancellation kills that group,
	// including any future vaultfile worker, never a shared parent session.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	killOwnedGroup := func() error {
		if cmd.Process == nil || cmd.Process.Pid <= 1 {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.Cancel = killOwnedGroup
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.CombinedOutput()
	if err != nil {
		_ = killOwnedGroup()
	}
	// Normal CloseWorkers completion must also leave no descendants. Bound
	// the check and clean up before reporting failure or inspecting sentinels.
	if cmd.Process != nil {
		deadline := time.Now().Add(2 * time.Second)
		for {
			groupErr := syscall.Kill(-cmd.Process.Pid, 0)
			if errors.Is(groupErr, syscall.ESRCH) {
				break
			}
			if groupErr != nil || time.Now().After(deadline) {
				_ = killOwnedGroup()
				t.Fatalf("owned manifest process group did not exit: %v; subprocess error: %v\n%s", groupErr, err, out)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	assertManifestInventory(t, f.root, before)
	if err != nil {
		t.Fatalf("manifest subprocess: %v\n%s", err, out)
	}
	if coverageDir != "" {
		// Exercise the coverage-specific path rather than silently disabling
		// instrumentation or accepting a runner that produced no artifacts.
		for _, pattern := range []string{"covmeta.*", "covcounters.*"} {
			files, globErr := filepath.Glob(filepath.Join(coverageDir, pattern))
			if globErr != nil || len(files) == 0 {
				t.Fatalf("missing subprocess coverage artifacts %s: %v", pattern, globErr)
			}
		}
	}
	var result manifestProcessResult
	found := false
	for _, line := range strings.Split(string(out), "\n") {
		if raw, ok := strings.CutPrefix(line, manifestResultPrefix); ok {
			if found {
				t.Fatal("duplicate subprocess result")
			}
			if err := json.Unmarshal([]byte(raw), &result); err != nil {
				t.Fatal(err)
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("no manifest subprocess result: %s", out)
	}
	if result.Error != "" && !reflect.DeepEqual(result.Manifest, ReadOnlyLaunchManifest{}) {
		t.Fatalf("failure returned a partial manifest: %+v", result)
	}
	return result
}
func TestReadOnlyLaunchManifestProcess(t *testing.T) {
	if os.Getenv(manifestProcessMarker) == "1" {
		var request manifestProcessRequest
		if err := json.NewDecoder(os.Stdin).Decode(&request); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		var result manifestProcessResult
		var err error
		if request.ExpectedRevision == "" {
			result.Manifest, err = ResolveReadOnlyLaunchManifest(ctx, request.Selector, request.PrivateRoot)
		} else {
			result.Manifest, err = VerifyReadOnlyLaunchManifest(ctx, request.Selector, request.PrivateRoot, request.ExpectedRevision)
		}
		cancel()
		if err != nil {
			result.Error = err.Error()
			result.Changed = errors.Is(err, ErrLaunchManifestChanged)
		}
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 3*time.Second)
		if err := vaultfile.CloseWorkers(closeCtx); err != nil {
			t.Fatal(err)
		}
		closeCancel()
		raw, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Println(manifestResultPrefix + string(raw))
		return
	}
	t.Run("healthy-selected-peer", func(t *testing.T) {
		f := newManifestProcessFixture(t)
		request := manifestProcessRequest{Selector: LaunchSelector{Name: "work"}, PrivateRoot: f.private}
		first := manifestChild(t, f, request)
		if first.Error != "" {
			t.Fatal(first.Error)
		}
		if first.Manifest.Active.Name != "work" || first.Manifest.Active.CanonicalPath != f.work {
			t.Fatalf("wrong selected vault: %+v", first.Manifest.Active)
		}
		if first.Manifest.Active.OrdinaryDB == first.Manifest.Active.PrivateDB || !strings.HasPrefix(first.Manifest.Active.PrivateDB, f.private+string(os.PathSeparator)) {
			t.Fatalf("cache separation: %+v", first.Manifest.Active)
		}
		if first.Manifest.Context.Home != f.home || first.Manifest.Context.Cwd != f.cwd || first.Manifest.Context.XDGCacheHome != f.xdg || first.Manifest.Context.XDGConfigHome != f.home {
			t.Fatalf("unexpected resolution context: %+v", first.Manifest.Context)
		}
		if len(first.Manifest.Peers) != 2 || len(first.Manifest.Configurations) != 3 {
			t.Fatalf("incomplete provenance: %+v", first.Manifest)
		}
		remainingPeers := map[string]string{"main": f.main, "work": f.work}
		for _, peer := range first.Manifest.Peers {
			want, exists := remainingPeers[peer.Name]
			if !exists || peer.DisplayPath != want || peer.CanonicalPath != want || peer.RootIdentity == "" || !strings.HasPrefix(peer.OrdinaryDB, f.normal+string(os.PathSeparator)) || !strings.HasPrefix(peer.PrivateDB, filepath.Join(f.private, "canonical-read-only-v1")+string(os.PathSeparator)) {
				t.Fatalf("incorrect/duplicate peer/cache provenance: %+v", peer)
			}
			delete(remainingPeers, peer.Name)
		}
		if len(remainingPeers) != 0 {
			t.Fatalf("missing peers: %+v", remainingPeers)
		}
		remainingConfigs := map[string]bool{f.machine: true, filepath.Join(f.main, ".track", "config.yml"): true, filepath.Join(f.work, ".track", "config.yml"): true}
		for _, provenance := range first.Manifest.Configurations {
			if !remainingConfigs[provenance.Path] || provenance.CanonicalPath != provenance.Path || !provenance.Present || provenance.Identity == "" {
				t.Fatalf("incorrect/duplicate config provenance: %+v", provenance)
			}
			raw, err := os.ReadFile(provenance.Path)
			if err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(provenance.Path)
			if err != nil {
				t.Fatal(err)
			}
			if provenance.SHA256 != fmt.Sprintf("%x", sha256.Sum256(raw)) || provenance.Size != int64(len(raw)) || provenance.ModifiedNS != info.ModTime().UnixNano() || provenance.Mode != uint32(info.Mode()) {
				t.Fatalf("config bytes/metadata provenance differs: %+v", provenance)
			}
			delete(remainingConfigs, provenance.Path)
		}
		if len(remainingConfigs) != 0 {
			t.Fatalf("missing config provenance: %+v", remainingConfigs)
		}
		if first.Manifest.Revision == "" {
			t.Fatal("empty revision")
		}
		request.ExpectedRevision = first.Manifest.Revision
		verified := manifestChild(t, f, request)
		if verified.Error != "" || !reflect.DeepEqual(first.Manifest, verified.Manifest) {
			t.Fatalf("unchanged reverify differs: %+v %+v", first, verified)
		}
	})
	for _, changed := range []string{"machine", "peer-config"} {
		t.Run(changed+"-revision", func(t *testing.T) {
			f := newManifestProcessFixture(t)
			request := manifestProcessRequest{Selector: LaunchSelector{Name: "work"}, PrivateRoot: f.private}
			first := manifestChild(t, f, request)
			if first.Error != "" {
				t.Fatal(first.Error)
			}
			path := f.machine
			if changed == "peer-config" {
				path = filepath.Join(f.main, ".track", "config.yml")
			}
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			replacement := append(append([]byte(nil), original...), []byte("\n# exact-byte revision sentinel\n")...)
			expected := manifestInventory(t, f.root)
			rel, _ := filepath.Rel(f.root, path)
			row := expected[rel]
			row.Size = int64(len(replacement))
			row.Hash = sha256.Sum256(replacement)
			stamp := time.Unix(1578052800, 0)
			row.ModifiedNS = stamp.UnixNano()
			expected[rel] = row
			if err := os.WriteFile(path, replacement, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(path, stamp, stamp); err != nil {
				t.Fatal(err)
			}
			request.ExpectedRevision = first.Manifest.Revision
			result := manifestChild(t, f, request)
			if !result.Changed {
				t.Fatalf("configuration change did not invalidate revision: %+v", result)
			}
			assertManifestInventory(t, f.root, expected)
		})
	}
	t.Run("missing-peer", func(t *testing.T) {
		f := newManifestProcessFixture(t)
		missing := filepath.Join(f.root, "missing-peer")
		manifestWriteMachine(t, f, missing, f.normal)
		result := manifestChild(t, f, manifestProcessRequest{Selector: LaunchSelector{Name: "main"}, PrivateRoot: f.private})
		if result.Error == "" || !strings.Contains(result.Error, missing) {
			t.Fatalf("missing peer not specifically rejected: %+v", result)
		}
		if _, err := os.Lstat(missing); !os.IsNotExist(err) {
			t.Fatalf("missing peer created: %v", err)
		}
	})
	t.Run("private-alias", func(t *testing.T) {
		f := newManifestProcessFixture(t)
		alias := filepath.Join(f.root, "private-alias")
		if err := os.Symlink(f.private, alias); err != nil {
			t.Fatal(err)
		}
		result := manifestChild(t, f, manifestProcessRequest{Selector: LaunchSelector{Name: "work"}, PrivateRoot: alias})
		if result.Error == "" || result.Error != "private root must be canonical" {
			t.Fatalf("private alias not specifically rejected: %+v", result)
		}
	})
	t.Run("private-in-peer", func(t *testing.T) {
		f := newManifestProcessFixture(t)
		result := manifestChild(t, f, manifestProcessRequest{Selector: LaunchSelector{Name: "main"}, PrivateRoot: f.work})
		if result.Error == "" || !strings.Contains(result.Error, "overlap") {
			t.Fatalf("private/peer overlap not rejected: %+v", result)
		}
	})
	t.Run("private-config-overlap", func(t *testing.T) {
		f := newManifestProcessFixture(t)
		namespace := filepath.Join(f.private, "canonical-read-only-v1")
		if err := os.Mkdir(namespace, 0700); err != nil {
			t.Fatal(err)
		}
		moved := filepath.Join(namespace, "machine.yml")
		if err := os.Rename(f.machine, moved); err != nil {
			t.Fatal(err)
		}
		f.machine = moved
		result := manifestChild(t, f, manifestProcessRequest{Selector: LaunchSelector{Name: "work"}, PrivateRoot: f.private})
		if !strings.Contains(result.Error, "overlaps configuration") {
			t.Fatalf("config overlap not specifically rejected: %+v", result)
		}
	})
	t.Run("normal-cache-overlap", func(t *testing.T) {
		f := newManifestProcessFixture(t)
		manifestWriteMachine(t, f, f.work, filepath.Join(f.private, "canonical-read-only-v1"))
		result := manifestChild(t, f, manifestProcessRequest{Selector: LaunchSelector{Name: "work"}, PrivateRoot: f.private})
		if !strings.Contains(result.Error, "overlaps a normal index") {
			t.Fatalf("normal cache overlap not specifically rejected: %+v", result)
		}
	})

}

package config

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/vaultfile"
)

func TestOwnedConfigurationReaderCannotOpenIndexOrAuthorizeUnknownNamespace(t *testing.T) {
	const childMarker = "GO_CONFIG_READER_TEST_CHILD"
	const completed = "CONFIG_READER_TEST_COMPLETE"
	if os.Getenv(childMarker) != "1" {
		// The worker pool captures process environment and cannot be restarted
		// after CloseWorkers. Each -count iteration needs its own process, not
		// a new fixture attached to a previous iteration's live workers.
		runner := t.TempDir()
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		args := []string{"-test.run=^TestOwnedConfigurationReaderCannotOpenIndexOrAuthorizeUnknownNamespace$", "-test.count=1"}
		if testing.CoverMode() != "" {
			args = append(args, "-test.gocoverdir="+t.TempDir())
		}
		cmd := exec.CommandContext(ctx, exe, args...)
		cmd.Dir = runner
		cmd.Env = []string{"PATH=/usr/bin:/bin", childMarker + "=1", "HOME=" + runner, "XDG_CONFIG_HOME=" + runner, "XDG_CACHE_HOME=" + runner, "TMPDIR=" + runner}
		killGroup := configureReaderTestProcess(cmd)
		cmd.Cancel = killGroup
		cmd.WaitDelay = 2 * time.Second
		out, err := cmd.CombinedOutput()
		if err != nil {
			_ = killGroup()
		}
		if cmd.Process != nil {
			deadline := time.Now().Add(2 * time.Second)
			for {
				alive, groupErr := readerTestProcessGroupAlive(cmd)
				if !alive && groupErr == nil {
					break
				}
				if groupErr != nil || time.Now().After(deadline) {
					_ = killGroup()
					t.Fatalf("configuration test process group did not exit: %v\n%s", groupErr, out)
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
		if err != nil || !strings.Contains(string(out), completed+"\n") {
			t.Fatalf("configuration reader subprocess did not complete: %v\n%s", err, out)
		}
		return
	}
	root := t.TempDir()
	cache := t.TempDir()
	machine := filepath.Join(t.TempDir(), "machine.yml")
	os.MkdirAll(filepath.Join(root, ".track", "notes"), 0700)
	os.WriteFile(filepath.Join(root, ".track", "config.yml"), []byte("link_format: id-v1\n"), 0600)
	bad := filepath.Join(root, ".track", "notes", "1.yaml")
	original := []byte("version: 1\ntitle: 'id:123'\n")
	os.WriteFile(bad, original, 0600)
	os.WriteFile(machine, []byte("vault_dir: "+root+"\ncache_dir: "+cache+"\n"), 0600)
	t.Setenv("TRACK_CONFIG", machine)
	t.Setenv("TRACK_VAULT", root)
	t.Setenv("TRACK_CACHE_DIR", "")
	// Registered after fixture/environment cleanups, so workers are reaped
	// before their captured paths disappear or their environment is restored.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := vaultfile.CloseWorkers(ctx); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	provisional, err := LoadBounded(ctx, "", false)
	if err != nil || provisional == nil || !provisional.Provisional {
		t.Fatalf("read-only config unavailable: %+v %v", provisional, err)
	}
	if _, err := LoadBounded(ctx, "", true); err == nil {
		t.Fatal("unverified ID namespace authorized writes")
	}
	if _, err := os.Stat(provisional.DBPath); !os.IsNotExist(err) {
		t.Fatal("configuration worker opened index", err)
	}
	raw, _ := os.ReadFile(bad)
	if string(raw) != string(original) {
		t.Fatal("config worker mutated sidecar")
	}
	fmt.Println(completed)
}

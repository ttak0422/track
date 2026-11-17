//go:build linux || darwin

package webui

// Test-binary-only bridge for the native owned-process integration. This is not
// a public CLI, production admission path, or an authorization from a manifest.
// Both phases independently require the exact disposable fixture/environment.
import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/vaultfile"
)

func nativeBoundFixture(root string) error {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || !strings.HasPrefix(filepath.Base(root), "track-native-registry-") {
		return errors.New("explicit disposable fixture root required")
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil || canonical != root {
		return errors.New("fixture root must be canonical")
	}
	expected := map[string]string{
		"HOME": filepath.Join(root, "home"), "XDG_CACHE_HOME": filepath.Join(root, "xdg"),
		"XDG_CONFIG_HOME": filepath.Join(root, "private"), "TMPDIR": filepath.Join(root, "private"), "TRACK_CONFIG": filepath.Join(root, "config"),
	}
	for key, value := range expected {
		if os.Getenv(key) != value {
			return fmt.Errorf("unexpected fixture environment: %s", key)
		}
	}
	cwd, err := os.Getwd()
	if err != nil || cwd != filepath.Join(root, "cwd") {
		return errors.New("unexpected fixture cwd")
	}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "TRACK_") && key != "TRACK_CONFIG" && key != "TRACK_WEB_DESKTOP_PARENT_PID" && key != "TRACK_WEB_DESKTOP_READY_TOKEN" {
			return fmt.Errorf("unexpected resolution variable: %s", key)
		}
	}
	// Validate types before reading; no symlinks, hardlinks or special-file opens.
	// Bounded synthetic trees only. This is not hostile replacement protection.
	count, totalBytes := 0, int64(0)
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		count++
		if count > 4096 {
			return errors.New("fixture entry bound exceeded")
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || int(stat.Uid) != os.Geteuid() || info.Mode().Perm()&0022 != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return fmt.Errorf("unsafe fixture entry: %s", path)
		}
		if info.Mode().IsRegular() {
			totalBytes += info.Size()
			if stat.Nlink != 1 || info.Size() > 1<<20 || totalBytes > 8<<20 {
				return errors.New("fixture file bound or link violation")
			}
		}
		return nil
	}); err != nil {
		return err
	}
	for _, rel := range []string{"home", "home/main", "home/main/.track", "home/main/.track/notes", "home/main/note", "home/work", "home/work/.track", "home/work/.track/notes", "home/work/note", "home/normal", "xdg", "cwd", "private"} {
		info, err := os.Lstat(filepath.Join(root, rel))
		if err != nil || !info.IsDir() {
			return fmt.Errorf("missing fixture directory: %s", rel)
		}
	}
	info, err := os.Lstat(filepath.Join(root, "private"))
	if err != nil || info.Mode().Perm() != 0700 {
		return errors.New("fixture cache must be private")
	}
	read := func(rel string) ([]byte, error) {
		path := filepath.Join(root, rel)
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
			return nil, fmt.Errorf("fixture input must be bounded regular file: %s", rel)
		}
		return os.ReadFile(path)
	}
	marker, err := read(".track-native-read-fixture")
	if err != nil || string(marker) != "disposable-fixture-only\n" {
		return errors.New("disposable marker required")
	}
	for _, name := range []string{"main", "work"} {
		raw, err := read("home/" + name + "/.track/config.yml")
		if err != nil || string(raw) != "link_format: id-v1\n" {
			return errors.New("unexpected fixture peer configuration")
		}
	}
	raw, err := read("config")
	if err != nil {
		return err
	}
	want := map[string]any{"default_vault": "main", "cache_dir": filepath.Join(root, "home", "normal"), "vaults": map[string]any{"main": filepath.Join(root, "home", "main"), "work": filepath.Join(root, "home", "work")}}
	expectedJSON, err := json.Marshal(want)
	if err != nil || !bytes.Equal(raw, expectedJSON) {
		return errors.New("fixture configuration differs from generated registry")
	}
	return nil
}

func nativeBoundOwner() (context.Context, context.CancelFunc, string, error) {
	pid, err := strconv.Atoi(os.Getenv("TRACK_WEB_DESKTOP_PARENT_PID"))
	if err != nil || pid <= 1 || os.Getppid() != pid {
		return nil, nil, "", errors.New("fixture requires actual owning parent")
	}
	token := os.Getenv("TRACK_WEB_DESKTOP_READY_TOKEN")
	if len(token) != 64 {
		return nil, nil, "", errors.New("fixture requires bounded ready token")
	}
	if _, err = hex.DecodeString(token); err != nil {
		return nil, nil, "", err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if os.Getppid() != pid {
					cancel()
					return
				}
			}
		}
	}()
	return ctx, cancel, token, nil
}

func TestNativeBoundFixtureBridge(t *testing.T) {
	mode := os.Getenv("GO_TRACK_BOUND_MODE")
	if mode == "" {
		t.Skip("native fixture bridge requires explicit capture/serve invocation")
	}
	if mode != "capture" && mode != "serve" {
		t.Fatal("unsupported native fixture mode")
	}
	root := os.Getenv("GO_TRACK_BOUND_ROOT")
	if os.Getenv("GO_TRACK_BOUND_SELECTED") != "work" {
		t.Fatal("fixture selector must be work")
	}
	if err := nativeBoundFixture(root); err != nil {
		t.Fatal(err)
	}
	ctx, cancel, token, err := nativeBoundOwner()
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		if err := vaultfile.CloseWorkers(cleanup); err != nil {
			t.Error(err)
		}
	}()
	rawRequest := []byte(os.Getenv("GO_TRACK_BOUND_REQUEST"))
	request, err := parseBoundLaunch(rawRequest)
	expectedMode := "serve"
	if mode == "capture" {
		expectedMode = "preflight"
	}
	if err != nil || *request.Mode != expectedMode || *request.Selector.Name != "work" || *request.Selector.Path != "" || *request.PrivateRoot != filepath.Join(root, "private") {
		t.Fatal("fixture request does not match bridge invocation")
	}
	if mode == "capture" {
		result, err := dispatchBoundLaunch(ctx, rawRequest, nil)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(result.Manifest)
		if err != nil || len(raw) > 128<<10 {
			t.Fatal("manifest response exceeds fixture bound", err)
		}
		fmt.Printf("TRACK_BOUND_MANIFEST:%s\n", raw)
		return
	}
	addr := os.Getenv("GO_TRACK_BOUND_ADDR")
	host, port, err := net.SplitHostPort(addr)
	if err != nil || host != "127.0.0.1" {
		t.Fatal("fixture requires explicit IPv4 loopback address")
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1024 || number >= 65535 {
		t.Fatal("invalid fixture listener port")
	}
	result, err := dispatchBoundLaunch(ctx, rawRequest, func(cfg *config.Config) (func(), error) { return AcquireWebPID(cfg, addr) })
	if err != nil {
		t.Fatal(err)
	}
	b := result.Bootstrap
	defer b.Close()
	if err = ServeBootstrapWithLifecycle(ctx, b, addr, func() { fmt.Fprintf(os.Stderr, "TRACK_DESKTOP_READY:%s\n", token) }); err != nil {
		t.Fatal(err)
	}
}

// Each call is a fresh fixture; no config/index workers run in these tests.
func nativeBoundValidationFixture(t *testing.T) string {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "track-native-registry-test")
	for _, rel := range []string{"home/main/.track/notes", "home/main/note", "home/work/.track/notes", "home/work/note", "home/normal", "private", "xdg", "cwd"} {
		if err = os.MkdirAll(filepath.Join(root, rel), 0700); err != nil {
			t.Fatal(err)
		}
	}
	write := func(rel string, raw []byte) {
		if err := os.WriteFile(filepath.Join(root, rel), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(".track-native-read-fixture", []byte("disposable-fixture-only\n"))
	for _, name := range []string{"main", "work"} {
		write("home/"+name+"/.track/config.yml", []byte("link_format: id-v1\n"))
	}
	raw, _ := json.Marshal(map[string]any{"default_vault": "main", "cache_dir": filepath.Join(root, "home/normal"), "vaults": map[string]string{"main": filepath.Join(root, "home/main"), "work": filepath.Join(root, "home/work")}})
	write("config", raw)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "TRACK_") {
			t.Setenv(key, "")
			if err := os.Unsetenv(key); err != nil {
				t.Fatal(err)
			}
		}
	}
	for key, value := range map[string]string{"HOME": filepath.Join(root, "home"), "XDG_CACHE_HOME": filepath.Join(root, "xdg"), "XDG_CONFIG_HOME": filepath.Join(root, "private"), "TMPDIR": filepath.Join(root, "private"), "TRACK_CONFIG": filepath.Join(root, "config")} {
		t.Setenv(key, value)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chdir(filepath.Join(root, "cwd")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(cwd); err != nil {
			t.Error(err)
		}
	})
	return root
}
func TestNativeBoundFixtureValidation(t *testing.T) {
	for _, kind := range []string{"valid", "missing-marker", "external-registry", "symlink", "hardlink", "fifo", "shared", "peer-config", "environment", "cwd", "large"} {
		t.Run(kind, func(t *testing.T) {
			root := nativeBoundValidationFixture(t)
			var mutationErr error
			switch kind {
			case "missing-marker":
				mutationErr = os.Remove(filepath.Join(root, ".track-native-read-fixture"))
			case "external-registry":
				mutationErr = os.WriteFile(filepath.Join(root, "config"), []byte(`{"default_vault":"main","vaults":{"main":"/outside"}}`), 0600)
			case "symlink":
				mutationErr = os.Symlink("/outside", filepath.Join(root, "escaped"))
			case "hardlink":
				mutationErr = os.Link(filepath.Join(root, "config"), filepath.Join(root, "alias"))
			case "fifo":
				mutationErr = syscall.Mkfifo(filepath.Join(root, "pipe"), 0600)
			case "shared":
				mutationErr = os.Chmod(filepath.Join(root, "private"), 0777)
			case "peer-config":
				mutationErr = os.WriteFile(filepath.Join(root, "home/work/.track/config.yml"), []byte("cache_dir: /outside\n"), 0600)
			case "environment":
				t.Setenv("TRACK_VAULT", "/outside")
			case "cwd":
				mutationErr = os.Chdir(root)
			case "large":
				f, err := os.Create(filepath.Join(root, "large"))
				if err != nil {
					t.Fatal(err)
				}
				err = f.Truncate(1<<20 + 1)
				f.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			if mutationErr != nil {
				t.Fatal(mutationErr)
			}
			err := nativeBoundFixture(root)
			if (err == nil) != (kind == "valid") {
				t.Fatalf("%s: %v", kind, err)
			}
		})
	}
}

func TestNativeBoundOwnerRequiresRealParentAndToken(t *testing.T) {
	for _, kind := range []string{"valid", "wrong-parent", "bad-token", "missing-token"} {
		t.Run(kind, func(t *testing.T) {
			t.Setenv("TRACK_WEB_DESKTOP_PARENT_PID", strconv.Itoa(os.Getppid()))
			t.Setenv("TRACK_WEB_DESKTOP_READY_TOKEN", strings.Repeat("a", 64))
			switch kind {
			case "wrong-parent":
				t.Setenv("TRACK_WEB_DESKTOP_PARENT_PID", "1")
			case "bad-token":
				t.Setenv("TRACK_WEB_DESKTOP_READY_TOKEN", strings.Repeat("z", 64))
			case "missing-token":
				t.Setenv("TRACK_WEB_DESKTOP_READY_TOKEN", "")
			}
			ctx, cancel, token, err := nativeBoundOwner()
			if cancel != nil {
				defer cancel()
			}
			if (err == nil) != (kind == "valid") {
				t.Fatalf("%s: %v", kind, err)
			}
			if err == nil {
				if token != strings.Repeat("a", 64) {
					t.Fatal("token changed")
				}
				cancel()
				select {
				case <-ctx.Done():
				case <-time.After(time.Second):
					t.Fatal("owner context not canceled")
				}
			}
		})
	}
}

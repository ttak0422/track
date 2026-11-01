package cli

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/track/backup"
	"github.com/ttak0422/track/internal/track/config"
)

// Intercept before normal config loading so all source I/O stays in a disposable
// worker. Preserve global --vault before the command as well as after it.
func backupArgs(args []string) ([]string, bool) {
	for i := 0; i < len(args); i++ {
		if args[i] == "--vault" {
			i++
			continue
		}
		if strings.HasPrefix(args[i], "--vault=") {
			continue
		}
		if args[i] != "backup" {
			return nil, false
		}
		return append(append([]string{}, args[:i]...), args[i+1:]...), true
	}
	return nil, false
}

type backupOptions struct {
	out, vault     string
	all, overwrite bool
	timeout        time.Duration
	stage          string
}

func parseBackup(args []string, worker bool) (backupOptions, int, bool) {
	var o backupOptions
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	fs.StringVar(&o.out, "out", "", "destination .tar.gz outside all source vaults (parent directory must exist)")
	fs.StringVar(&o.vault, "vault", "", "registered vault name (default: active vault)")
	fs.BoolVar(&o.all, "all-vaults", false, "include active and all registered vaults in separate directories")
	fs.BoolVar(&o.overwrite, "overwrite", false, "atomically replace an existing regular archive after success")
	fs.DurationVar(&o.timeout, "timeout", 5*time.Minute, "total worker deadline, including source reads; must be positive")
	if worker {
		fs.StringVar(&o.stage, "stage", "", "internal staging basename")
	}
	if code, ok := parseArgs(fs, args); !ok {
		return o, code, false
	}
	if fs.NArg() != 0 || o.out == "" {
		return o, fail("backup requires --out FILE.tar.gz and no positional arguments"), false
	}
	if !strings.HasSuffix(o.out, ".tar.gz") {
		return o, fail("backup --out must end in .tar.gz"), false
	}
	if o.timeout <= 0 {
		return o, fail("backup --timeout must be positive"), false
	}
	if o.all && o.vault != "" {
		return o, fail("backup --all-vaults and --vault cannot be combined"), false
	}
	return o, 0, true
}

func cmdBackup(args []string) int {
	o, code, ok := parseBackup(args, false)
	if !ok {
		return code
	}
	output, err := filepath.Abs(o.out)
	if err != nil {
		return fail("%v", err)
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(output))
	if err != nil {
		return fail("backup destination directory: %v", err)
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		return fail("backup destination directory: %v", err)
	}
	defer root.Close()
	base := filepath.Base(output)
	if err := checkBackupDestination(root, base, o.overwrite); err != nil {
		return fail("%v", err)
	}
	stage := ".track-backup-" + rand.Text() + ".partial"
	defer root.Remove(stage)
	exe, err := os.Executable()
	if err != nil {
		return fail("%v", err)
	}
	workerArgs := []string{"__backup", "--out", filepath.Join(parent, base), "--stage", stage}
	if o.vault != "" {
		workerArgs = append(workerArgs, "--vault", o.vault)
	}
	if o.all {
		workerArgs = append(workerArgs, "--all-vaults")
	}
	cmd := exec.Command(exe, workerArgs...)
	var result bytes.Buffer
	cmd.Stdout = &result
	cmd.Stderr = &result
	if err := waitBackupWorker(cmd, o.timeout); err != nil {
		return fail("backup incomplete: %v; previous archive preserved", err)
	}
	var report struct {
		Complete bool   `json:"complete"`
		Files    int    `json:"files"`
		Vaults   int    `json:"vaults"`
		Error    string `json:"error"`
	}
	if err := json.Unmarshal(result.Bytes(), &report); err != nil {
		return fail("backup worker returned an invalid result; previous archive preserved")
	}
	if !report.Complete {
		return fail("backup incomplete: %s; previous archive preserved", report.Error)
	}
	if err := checkBackupDestination(root, base, o.overwrite); err != nil {
		return fail("%v", err)
	}
	if o.overwrite {
		err = root.Rename(stage, base)
	} else {
		err = commitBackupExclusive(root, stage, base)
	}
	if err != nil {
		return fail("commit backup: %v", err)
	}
	return emit(map[string]any{"path": o.out, "complete": true, "format_version": backup.FormatVersion, "files": report.Files, "vaults": report.Vaults})
}

func checkBackupDestination(root *os.Root, name string, overwrite bool) error {
	info, err := root.Lstat(name)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("backup destination must be a regular file, never a symlink or directory")
	}
	if !overwrite {
		return fmt.Errorf("backup destination exists; use another name or --overwrite")
	}
	return nil
}

func waitBackupWorker(cmd *exec.Cmd, timeout time.Duration) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-done:
		// Operational failures are JSON; abnormal exits fail regardless of stdout.
		return err
	case <-timer.C:
		cmd.Process.Kill()
		// Do not wait for an uninterruptible read. Only the parent can publish the
		// final path; the worker's unlinked stage cannot replace a previous backup.
		return fmt.Errorf("worker timed out after %s; filesystem may still be unavailable", timeout)
	}
}

func cmdBackupWorker(args []string) int {
	o, code, ok := parseBackup(args, true)
	if !ok {
		return code
	}
	if !strings.HasPrefix(o.stage, ".track-backup-") || filepath.Base(o.stage) != o.stage || !strings.HasSuffix(o.stage, ".partial") {
		return fail("invalid backup stage")
	}
	vaults, err := backupVaults(o)
	if err != nil {
		return emit(map[string]any{"complete": false, "error": err.Error()})
	}
	manifest, err := backup.Write(vaults, filepath.Join(filepath.Dir(o.out), o.stage), config.ConfigPath())
	if err != nil {
		return emit(map[string]any{"complete": false, "error": err.Error()})
	}
	files := 0
	for _, entry := range manifest.Entries {
		if entry.Kind == "file" {
			files++
		}
	}
	return emit(map[string]any{"complete": true, "files": files, "vaults": len(vaults)})
}

func backupVaults(o backupOptions) ([]backup.Vault, error) {
	active, registry, err := config.VaultLocation()
	if err != nil {
		// YAML type errors can quote secret-bearing legacy configuration values.
		return nil, fmt.Errorf("backup machine configuration is invalid or unavailable; check TRACK_CONFIG and the vault registry")
	}
	if o.vault != "" {
		selected, ok := registry[o.vault]
		if !ok {
			return nil, fmt.Errorf("unknown vault %q", o.vault)
		}
		return []backup.Vault{{Name: o.vault, Prefix: "vault", Source: selected}}, nil
	}
	activeCanonical, err := filepath.EvalSymlinks(active)
	if err != nil {
		return nil, err
	}
	name := ""
	for n, p := range registry {
		if canonical, e := filepath.EvalSymlinks(p); e == nil && canonical == activeCanonical {
			name = n
		}
	}
	if !o.all {
		return []backup.Vault{{Name: name, Prefix: "vault", Source: active}}, nil
	}
	var vaults []backup.Vault
	if name == "" {
		vaults = append(vaults, backup.Vault{Prefix: "vaults/active", Source: active})
	}
	// Numeric prefixes avoid collisions between registered and active-vault names.
	for i, n := range vaultNames(registry) {
		vaults = append(vaults, backup.Vault{Name: n, Prefix: fmt.Sprintf("vaults/%04d", i+1), Source: registry[n]})
	}
	return vaults, nil
}

// Package vaultfile distinguishes individual unreadable inputs from write and vault-wide failures.
package vaultfile

import (
	"container/list"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

// Error is a file-local input failure. Resource exhaustion and all writes remain ordinary errors.
type Error struct {
	Path   string
	Reason string
	Err    error
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s", e.Path, e.Reason) }
func (e *Error) Unwrap() error { return e.Err }

// ReadError classifies only errors from reading or inspecting an input, never errors from writes.
func ReadError(path string, err error) error {
	reason := ""
	switch {
	case errors.Is(err, os.ErrNotExist):
		reason = "file or symlink target is missing"
	case errors.Is(err, os.ErrPermission):
		reason = "permission denied while reading file"
	case errors.Is(err, syscall.EIO):
		reason = "I/O error while reading file"
	case errors.Is(err, syscall.ESTALE):
		reason = "stale file handle"
	case errors.Is(err, syscall.ELOOP):
		reason = "symlink loop"
	case errors.Is(err, syscall.EISDIR):
		reason = "not a regular text file"
	case errors.Is(err, syscall.ENOTDIR):
		reason = "file path has a non-directory component"
	case errors.Is(err, syscall.ENXIO):
		reason = "file is unavailable"
	}
	if reason != "" {
		return &Error{Path: path, Reason: reason, Err: err}
	}
	return err
}

// Read accepts regular UTF-8 text files, including symlinks to regular files. In particular,
// a named pipe or device with a note extension must not block the scanner waiting for input.
func Read(path string) ([]byte, error) {
	v, err := bounded(request{Op: "read", Path: path})
	return v.Data, err
}

func read(path string) (*os.File, []byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, nil, ReadError(path, err)
	}
	info, err := f.Stat()
	if err == nil && !info.Mode().IsRegular() {
		err = &Error{Path: path, Reason: "not a regular text file"}
	}
	if err != nil {
		f.Close()
		return nil, nil, ReadError(path, err)
	}
	raw, err := io.ReadAll(io.LimitReader(f, MaxReadBytes+1))
	if err == nil && len(raw) > MaxReadBytes {
		err = &Error{Path: path, Reason: "text file exceeds read byte budget"}
	}
	if err == nil && !utf8.Valid(raw) {
		err = &Error{Path: path, Reason: "invalid UTF-8 text"}
	}
	if err != nil {
		f.Close()
		return nil, nil, ReadError(path, err)
	}
	return f, raw, nil
}

const diagnosticCacheLimit = 1024
const diagnosticBurstLimit = 128

type diagnosticEntry struct{ path, reason string }

var diagnostics = struct {
	sync.Mutex
	files      map[string]*list.Element
	order      *list.List
	window     time.Time
	emitted    int
	summarized bool
}{files: make(map[string]*list.Element), order: list.New()}

// Warn bounds retained paths and output during a malformed-file burst. Only fixed labels are
// accepted as reasons: parser errors may contain note text or metadata values.
func Warn(path, reason string) {
	diagnostics.Lock()
	defer diagnostics.Unlock()
	if entry := diagnostics.files[path]; entry != nil {
		if entry.Value.(diagnosticEntry).reason == reason {
			return
		}
		entry.Value = diagnosticEntry{path, reason}
		diagnostics.order.MoveToBack(entry)
	} else {
		if len(diagnostics.files) >= diagnosticCacheLimit {
			oldest := diagnostics.order.Front()
			delete(diagnostics.files, oldest.Value.(diagnosticEntry).path)
			diagnostics.order.Remove(oldest)
		}
		diagnostics.files[path] = diagnostics.order.PushBack(diagnosticEntry{path, reason})
	}
	if time.Since(diagnostics.window) >= time.Minute {
		diagnostics.window, diagnostics.emitted, diagnostics.summarized = time.Now(), 0, false
	}
	if diagnostics.emitted >= diagnosticBurstLimit {
		if !diagnostics.summarized {
			fmt.Fprintln(os.Stderr, "track: warning: additional file diagnostics suppressed; run 'track doctor' to inspect vault files")
			diagnostics.summarized = true
		}
		return
	}
	diagnostics.emitted++
	fmt.Fprintf(os.Stderr, "track: warning: skipping %q: %s; run 'track doctor' to inspect vault files\n", path, reason)
}

func Recovered(path string) {
	diagnostics.Lock()
	defer diagnostics.Unlock()
	if entry := diagnostics.files[path]; entry != nil {
		diagnostics.order.Remove(entry)
		delete(diagnostics.files, path)
	}
}

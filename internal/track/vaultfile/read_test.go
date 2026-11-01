package vaultfile

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestReadErrorClassification(t *testing.T) {
	for _, cause := range []error{os.ErrPermission, os.ErrNotExist, syscall.EIO, syscall.ESTALE, syscall.ELOOP} {
		err := ReadError("fixture", cause)
		var local *Error
		if !errors.As(err, &local) || !errors.Is(err, cause) {
			t.Fatalf("lost file-local cause: %v", err)
		}
	}
	for _, cause := range []error{syscall.EMFILE, syscall.ENFILE, syscall.ENOMEM, syscall.ENOSPC} {
		var local *Error
		if errors.As(ReadError("fixture", cause), &local) {
			t.Fatalf("system failure was skippable: %v", cause)
		}
	}
}

func TestRegularTextAndSymlink(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(path, []byte("healthy 日本語\n"), 0600); err != nil {
		t.Fatal(err)
	}
	link := path + ".link"
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	raw, err := Read(link)
	if err != nil || string(raw) != "healthy 日本語\n" {
		t.Fatalf("regular link = %q, %v", raw, err)
	}
}

func TestSpecialFileSymlinkTargetsAreRejectedWithoutBlocking(t *testing.T) {
	for _, kind := range []string{"pipe", "directory", "broken", "loop"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			target := filepath.Join(root, "target")
			path := filepath.Join(root, "note.md")
			switch kind {
			case "pipe":
				if err := syscall.Mkfifo(target, 0600); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(target, 0700); err != nil {
					t.Fatal(err)
				}
			case "loop":
				target = path
			}
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
			var local *Error
			if _, err := Read(path); !errors.As(err, &local) {
				t.Fatalf("special read = %v", err)
			}

		})
	}
}

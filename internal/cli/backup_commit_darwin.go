package cli

import (
	"golang.org/x/sys/unix"
	"os"
)

// Atomic no-replace rename also works on volumes without hard links (e.g. exFAT).
func commitBackupExclusive(root *os.Root, from, to string) error {
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	return unix.RenameatxNp(int(dir.Fd()), from, int(dir.Fd()), to, unix.RENAME_EXCL)
}

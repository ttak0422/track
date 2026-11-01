//go:build !darwin && !linux

package cli

import "os"

func commitBackupExclusive(root *os.Root, from, to string) error {
	return root.Link(from, to)
}

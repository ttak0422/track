//go:build linux || darwin

package config

import (
	"fmt"
	"io/fs"
	"syscall"
)

func manifestFileIdentity(info fs.FileInfo) (string, error) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("unsupported file identity")
	}
	return fmt.Sprintf("%d:%d", st.Dev, st.Ino), nil
}

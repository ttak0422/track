//go:build linux || darwin

package config

import (
	"io/fs"
	"os"
	"syscall"
)

const cacheIdentitySupported = true

func cacheOwned(info fs.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == os.Geteuid()
}
func cacheSingleLink(info fs.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Nlink == 1
}

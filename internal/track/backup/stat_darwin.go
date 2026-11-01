package backup

import (
	"io/fs"
	"syscall"
)

func isPlaceholder(info fs.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	// UF_DATALESS: refuse opening an evicted iCloud/File Provider file.
	return ok && st.Flags&0x40000000 != 0
}
func changeTime(info fs.FileInfo) any {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return st.Ctimespec
	}
	return nil
}

//go:build !darwin

package backup

import (
	"io/fs"
	"syscall"
)

// Other providers may expose placeholders as ordinary files; reads still run in
// the time-limited worker, but local availability must be verified by the user.
func isPlaceholder(info fs.FileInfo) bool { return false }
func changeTime(info fs.FileInfo) any {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return st.Ctim
	}
	return nil
}

package backup

import (
	"io/fs"
	"syscall"
	"testing"
)

type darwinInfo struct {
	fs.FileInfo
	stat syscall.Stat_t
}

func (s darwinInfo) Sys() any { return &s.stat }

func TestDatalessPlaceholderFlag(t *testing.T) {
	if !isPlaceholder(darwinInfo{stat: syscall.Stat_t{Flags: 0x40000000}}) {
		t.Fatal("missed UF_DATALESS")
	}
	if isPlaceholder(darwinInfo{stat: syscall.Stat_t{Flags: 0x00000020}}) {
		t.Fatal("ordinary file was treated as unavailable")
	}
}

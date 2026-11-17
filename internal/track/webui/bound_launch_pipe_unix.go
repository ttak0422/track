//go:build linux || darwin

package webui

import (
	"context"
	"errors"
	"os"
	"syscall"
	"time"
)

var errBoundLaunchPipe = errors.New("bound launch pipe rejected")

// readBoundLaunchPipe owns and closes pipe on every return. Only bounded,
// deadline-controlled anonymous/named pipe input is accepted. It never switches
// to an environment payload or starts a background reader.
func readBoundLaunchPipe(ctx context.Context, pipe *os.File) ([]byte, error) {
	if pipe == nil {
		return nil, errBoundLaunchPipe
	}
	defer pipe.Close()
	if ctx == nil {
		return nil, errBoundLaunchPipe
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, errBoundLaunchPipe
	}
	if ctx.Err() != nil {
		return nil, errBoundLaunchPipe
	}
	info, err := pipe.Stat()
	if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		return nil, errBoundLaunchPipe
	}
	// File.Fd may put a pollable descriptor into blocking mode. Capture exactly
	// once, then use only raw syscalls until the deferred File.Close.
	fd := int(pipe.Fd())
	if err = syscall.SetNonblock(fd, true); err != nil {
		return nil, errBoundLaunchPipe
	}
	const limit = 256 << 10
	result := make([]byte, 0, 4096)
	var chunk [4096]byte
	for {
		if ctx.Err() != nil {
			return nil, errBoundLaunchPipe
		}
		remaining := limit + 1 - len(result)
		size := len(chunk)
		if remaining < size {
			size = remaining
		}
		n, err := syscall.Read(fd, chunk[:size])
		if n > 0 {
			result = append(result, chunk[:n]...)
			if len(result) > limit {
				return nil, errBoundLaunchPipe
			}
		}
		if ctx.Err() != nil {
			return nil, errBoundLaunchPipe
		}
		if err == nil {
			if n == 0 {
				return result, nil
			}
			continue
		}
		if err == syscall.EINTR {
			continue
		}
		if err != syscall.EAGAIN && err != syscall.EWOULDBLOCK {
			return nil, errBoundLaunchPipe
		}
		timer := time.NewTimer(5 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, errBoundLaunchPipe
		case <-timer.C:
		}
	}
}

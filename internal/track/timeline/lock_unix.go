//go:build linux || darwin

package timeline

import (
	"context"
	"errors"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

func safeOpen(root *os.Root, name string, flag int, perm os.FileMode) (*os.File, error) {
	return root.OpenFile(name, flag|unix.O_NOFOLLOW|unix.O_NONBLOCK, perm)
}
func acquire(ctx context.Context, f *os.File) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			return err
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
func release(f *os.File) { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN) }

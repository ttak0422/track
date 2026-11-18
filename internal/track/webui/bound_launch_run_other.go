//go:build !linux && !darwin

package webui

import (
	"context"
	"io"
	"os"
)

// RunBoundReadLaunch is unavailable on platforms without the owned-pipe transport.
func RunBoundReadLaunch(_ context.Context, input *os.File, _ io.Writer, _ string, _ func()) error {
	if input != nil {
		input.Close()
	}
	return errBoundLaunchProtocol
}

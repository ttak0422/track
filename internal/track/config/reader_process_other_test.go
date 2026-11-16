//go:build !linux && !darwin

package config

import (
	"os"
	"os/exec"
)

// Non-Unix retains the reader regression without claiming Unix group cleanup.
// The child checks CloseWorkers before its fixtures are removed on normal exit.
func configureReaderTestProcess(cmd *exec.Cmd) func() error {
	return func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		return cmd.Process.Kill()
	}
}
func readerTestProcessGroupAlive(cmd *exec.Cmd) (bool, error) { return false, nil }

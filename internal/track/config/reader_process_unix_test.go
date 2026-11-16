//go:build linux || darwin

package config

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func configureReaderTestProcess(cmd *exec.Cmd) func() error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}
func readerTestProcessGroupAlive(cmd *exec.Cmd) (bool, error) {
	err := syscall.Kill(-cmd.Process.Pid, 0)
	if errors.Is(err, syscall.ESRCH) {
		return false, nil
	}
	return true, err
}

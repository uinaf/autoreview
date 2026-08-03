//go:build darwin || linux

package processgroup

import (
	"errors"
	"os/exec"
	"syscall"
)

func configure(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminate(leaderPID int) error {
	err := syscall.Kill(-leaderPID, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

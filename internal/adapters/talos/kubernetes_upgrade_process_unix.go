//go:build !windows

package talos

import (
	"os/exec"
	"syscall"
)

func configureUpgradeProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminateUpgradeProcess(command *exec.Cmd) error {
	if command.Process == nil {
		return nil
	}
	err := syscall.Kill(-command.Process.Pid, syscall.SIGTERM)
	if err == syscall.ESRCH {
		return nil
	}
	return err
}

func killUpgradeProcess(command *exec.Cmd) error {
	if command.Process == nil {
		return nil
	}
	err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	if err == syscall.ESRCH {
		return nil
	}
	return err
}

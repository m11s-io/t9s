//go:build windows

package talos

import "os/exec"

func configureUpgradeProcess(*exec.Cmd) {}

func terminateUpgradeProcess(command *exec.Cmd) error {
	if command.Process == nil {
		return nil
	}
	return command.Process.Kill()
}

func killUpgradeProcess(command *exec.Cmd) error {
	if command.Process == nil {
		return nil
	}
	return command.Process.Kill()
}

//go:build unix

package tools

import (
	"os/exec"
	"syscall"
)

func configureBashCmd(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	// Setpgid puts the shell in a group whose id is its pid. Signal that id
	// directly: Getpgid can fail after the shell exits even while descendants
	// in the group are still alive.
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		_ = cmd.Process.Kill()
	}
}

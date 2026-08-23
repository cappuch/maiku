//go:build windows

package tools

import (
	"context"
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

func configureBashCmd(cmd *exec.Cmd) {
	// Shell tools run behind the desktop UI and must not flash a console. Keep
	// any verbatim cmd.exe command line installed by shellcmd.
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
}

func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}

	// Process.Kill only terminates the shell and leaves grandchildren running.
	// taskkill /T is available on supported Windows versions and tears down the
	// complete tree before Wait reaps the shell.
	killCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	killer := exec.CommandContext(killCtx, "taskkill.exe", "/F", "/T", "/PID", strconv.Itoa(cmd.Process.Pid))
	killer.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := killer.Run(); err != nil {
		_ = cmd.Process.Kill()
	}
}

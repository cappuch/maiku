//go:build !windows

package mcp

import "os/exec"

func configureBackgroundCommand(cmd *exec.Cmd) {}

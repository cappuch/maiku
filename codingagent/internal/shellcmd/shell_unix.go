//go:build !windows

package shellcmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func platformCommand(config Config, commandText string) *exec.Cmd {
	args := append(append([]string(nil), config.Args...), commandText)
	return exec.Command(config.Executable, args...)
}

// Resolve returns the configured shell, then $SHELL, then sh.
func Resolve(shellPath string) Config {
	shell := strings.TrimSpace(shellPath)
	if shell == "" {
		shell = strings.TrimSpace(os.Getenv("SHELL"))
	}
	if shell == "" {
		shell = "sh"
	}

	name := strings.TrimSuffix(filepath.Base(shell), filepath.Ext(shell))
	if name == "" || name == "." {
		name = "POSIX shell"
	}
	return Config{Executable: shell, Args: []string{"-c"}, Name: name}
}

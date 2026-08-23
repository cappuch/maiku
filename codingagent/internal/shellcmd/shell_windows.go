//go:build windows

package shellcmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func platformCommand(config Config, commandText string) *exec.Cmd {
	base := strings.ToLower(filepath.Base(config.Executable))
	base = strings.TrimSuffix(base, filepath.Ext(base))
	if base == "cmd" {
		// cmd.exe does not use CommandLineToArgvW quoting. Pass the command line
		// verbatim using the same /D /S /C "command" form as Node's shell mode.
		// /C only executes the first physical line, so join multiline commands
		// with cmd's command separator. This also lets shellCommandPrefix and the
		// requested command run in the same shell process.
		commandText = strings.ReplaceAll(commandText, "\r\n", "\n")
		commandText = strings.ReplaceAll(commandText, "\r", "\n")
		lines := strings.Split(commandText, "\n")
		commands := lines[:0]
		for _, line := range lines {
			if strings.TrimSpace(line) != "" {
				commands = append(commands, line)
			}
		}
		commandText = strings.Join(commands, "&")

		cmd := exec.Command(config.Executable)
		cmd.SysProcAttr = &syscall.SysProcAttr{
			HideWindow: true,
			CmdLine:    quoteWindowsExecutable(config.Executable) + " " + strings.Join(config.Args, " ") + ` "` + commandText + `"`,
		}
		return cmd
	}

	args := append(append([]string(nil), config.Args...), commandText)
	cmd := exec.Command(config.Executable, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd
}

func quoteWindowsExecutable(executable string) string {
	return `"` + strings.ReplaceAll(executable, `"`, `""`) + `"`
}

// Resolve returns the explicitly configured shell or the native Windows
// command processor. Common custom shells receive their native argument form.
func Resolve(shellPath string) Config {
	shell := strings.TrimSpace(shellPath)
	if shell == "" {
		shell = strings.TrimSpace(os.Getenv("COMSPEC"))
	}
	if shell == "" {
		shell = "cmd.exe"
	}

	base := strings.ToLower(filepath.Base(shell))
	base = strings.TrimSuffix(base, filepath.Ext(base))
	switch base {
	case "cmd":
		return Config{Executable: shell, Args: []string{"/D", "/S", "/C"}, Name: "Windows Command Prompt"}
	case "powershell", "pwsh":
		return Config{
			Executable: shell,
			Args:       []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-Command"},
			Name:       "PowerShell",
		}
	default:
		// Git Bash, MSYS2, Cygwin, and other POSIX-style shells accept -c.
		return Config{Executable: shell, Args: []string{"-c"}, Name: filepath.Base(shell)}
	}
}

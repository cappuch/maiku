// Package shellcmd creates commands for the platform's native command shell.
package shellcmd

import "os/exec"

// Config describes how to invoke a command shell. Args contains the flags that
// must appear before the command text.
type Config struct {
	Executable string
	Args       []string
	Name       string
}

// Command creates a command that executes commandText with config.
func (config Config) Command(commandText string) *exec.Cmd {
	return platformCommand(config, commandText)
}

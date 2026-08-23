//go:build windows

package shellcmd

import (
	"reflect"
	"testing"
)

func TestResolveUsesCmdSyntaxByDefault(t *testing.T) {
	t.Setenv("COMSPEC", `C:\Windows\System32\cmd.exe`)
	config := Resolve("")
	if config.Executable != `C:\Windows\System32\cmd.exe` {
		t.Fatalf("executable = %q", config.Executable)
	}
	if !reflect.DeepEqual(config.Args, []string{"/D", "/S", "/C"}) {
		t.Fatalf("args = %#v", config.Args)
	}
}

func TestCmdCommandUsesVerbatimWindowsQuoting(t *testing.T) {
	config := Resolve(`C:\Windows\System32\cmd.exe`)
	commandText := `if "a b"=="a b" echo quoted-ok`
	cmd := config.Command(commandText)
	if cmd.SysProcAttr == nil {
		t.Fatal("cmd.exe command has no SysProcAttr")
	}
	want := `"C:\Windows\System32\cmd.exe" /D /S /C "if "a b"=="a b" echo quoted-ok"`
	if cmd.SysProcAttr.CmdLine != want {
		t.Fatalf("command line = %q, want %q", cmd.SysProcAttr.CmdLine, want)
	}
}

func TestCmdCommandJoinsMultilineCommands(t *testing.T) {
	config := Resolve(`C:\Windows\System32\cmd.exe`)
	cmd := config.Command("echo prefix\r\necho command")
	want := `"C:\Windows\System32\cmd.exe" /D /S /C "echo prefix&echo command"`
	if cmd.SysProcAttr == nil || cmd.SysProcAttr.CmdLine != want {
		t.Fatalf("command line = %q, want %q", cmd.SysProcAttr.CmdLine, want)
	}
}

func TestResolveRecognizesPowerShell(t *testing.T) {
	config := Resolve(`C:\Program Files\PowerShell\7\pwsh.exe`)
	if config.Name != "PowerShell" {
		t.Fatalf("name = %q, want PowerShell", config.Name)
	}
	if got := config.Args[len(config.Args)-1]; got != "-Command" {
		t.Fatalf("last arg = %q, want -Command", got)
	}
}

func TestResolveUsesDashCForCustomBash(t *testing.T) {
	config := Resolve(`C:\Program Files\Git\bin\bash.exe`)
	if !reflect.DeepEqual(config.Args, []string{"-c"}) {
		t.Fatalf("args = %#v", config.Args)
	}
}

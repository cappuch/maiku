//go:build !windows

package shellcmd

import (
	"os"
	"reflect"
	"testing"
)

func TestResolveUsesConfiguredUnixShell(t *testing.T) {
	config := Resolve(" /bin/zsh ")
	if config.Executable != "/bin/zsh" {
		t.Fatalf("executable = %q, want /bin/zsh", config.Executable)
	}
	if !reflect.DeepEqual(config.Args, []string{"-c"}) {
		t.Fatalf("args = %#v, want [-c]", config.Args)
	}
}

func TestResolveUsesShellEnvironment(t *testing.T) {
	old, hadOld := os.LookupEnv("SHELL")
	t.Cleanup(func() {
		if hadOld {
			_ = os.Setenv("SHELL", old)
		} else {
			_ = os.Unsetenv("SHELL")
		}
	})
	if err := os.Setenv("SHELL", "/test/shell"); err != nil {
		t.Fatal(err)
	}
	if got := Resolve("").Executable; got != "/test/shell" {
		t.Fatalf("executable = %q, want /test/shell", got)
	}
}

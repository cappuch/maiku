package core

import "testing"

func TestResolveConfigValueCommandUsesPlatformShell(t *testing.T) {
	if got := ResolveConfigValue("!echo maiku-auth-shell-test", nil); got != "maiku-auth-shell-test" {
		t.Fatalf("resolved command value = %q", got)
	}
}

//go:build unix

package core

import (
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestResolveConfigValueDoesNotWaitForBackgroundOutputHandle(t *testing.T) {
	started := time.Now()
	value := ResolveConfigValue("!sleep 10 & echo maiku-auth-background:$!", nil)
	parts := strings.Split(value, ":")
	if len(parts) != 2 || parts[0] != "maiku-auth-background" {
		t.Fatalf("resolved command value = %q", value)
	}
	pid, err := strconv.Atoi(parts[1])
	if err != nil {
		t.Fatalf("background pid = %q: %v", parts[1], err)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("auth command waited for background descendant for %v", elapsed)
	}
}

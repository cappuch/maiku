//go:build !windows

package tools

import (
	"context"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestBashToolTimeoutReturnsPromptly(t *testing.T) {
	started := time.Now()
	_, err := CreateBashTool(t.TempDir()).Execute(
		context.Background(),
		"test",
		map[string]any{"command": "sleep 10", "timeout": 0.05},
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("error = %v, want timeout", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("timeout returned after %v", elapsed)
	}
}

func TestBashToolDoesNotWaitForInheritedOutputHandle(t *testing.T) {
	started := time.Now()
	output, err := executeBashTool(t, CreateBashTool(t.TempDir()), "sleep 10 & echo $!")
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(output))
	if err != nil {
		t.Fatalf("background pid output = %q: %v", output, err)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("command waited for background descendant for %v", elapsed)
	}
}

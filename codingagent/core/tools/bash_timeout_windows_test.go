//go:build windows

package tools

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestBashToolPreservesCmdQuotes(t *testing.T) {
	output, err := executeBashTool(
		t,
		CreateBashTool(t.TempDir()),
		`if "a b"=="a b" echo quoted-ok`,
	)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(output) != "quoted-ok" {
		t.Fatalf("output = %q, want quoted-ok", output)
	}
}

func TestBashToolTimeoutReturnsPromptly(t *testing.T) {
	started := time.Now()
	_, err := CreateBashTool(t.TempDir()).Execute(
		context.Background(),
		"test",
		map[string]any{
			"command": "ping -n 30 127.0.0.1 >NUL",
			"timeout": 0.05,
		},
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("error = %v, want timeout", err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("timeout returned after %v", elapsed)
	}
}

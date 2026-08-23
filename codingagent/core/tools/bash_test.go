package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/mikus/maiku/agent"
)

func executeBashTool(t *testing.T, tool *agent.AgentTool, command string) (string, error) {
	t.Helper()
	result, err := tool.Execute(context.Background(), "test", map[string]any{"command": command}, nil)
	if err != nil {
		return "", err
	}
	if len(result.Content) != 1 {
		t.Fatalf("content length = %d, want 1", len(result.Content))
	}
	return result.Content[0].Text, nil
}

func TestBashToolExecutesWithPlatformDefaultShell(t *testing.T) {
	output, err := executeBashTool(t, CreateBashTool(t.TempDir()), "echo hello")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(output) != "hello" {
		t.Fatalf("output = %q, want hello", output)
	}
}

func TestBashToolAppliesCommandPrefix(t *testing.T) {
	tool := CreateBashToolWithOptions(t.TempDir(), BashOptions{CommandPrefix: "echo prefix"})
	output, err := executeBashTool(t, tool, "echo command")
	if err != nil {
		t.Fatal(err)
	}
	normalized := strings.ReplaceAll(strings.TrimSpace(output), "\r\n", "\n")
	if normalized != "prefix\ncommand" {
		t.Fatalf("output = %q, want prefix and command", output)
	}
}

package tools

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/cappuch/maiku/agent"
)

func TestBashResultTruncatesOutputAndPreservesFullOutputFile(t *testing.T) {
	for _, tc := range []struct {
		name   string
		output string
	}{
		{"bytes", "omitted-head\n" + strings.Repeat("x", 16*1024) + "\nfinal-error"},
		{"lines", "omitted-head\n" + strings.Repeat("line\n", 400) + "final-error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text, details, err := formatBashOutput(tc.output, "(no output)")
			if err != nil {
				t.Fatal(err)
			}
			if details == nil || details.Truncation == nil || !details.Truncation.Truncated {
				t.Fatal("expected truncated bash result")
			}
			t.Cleanup(func() { _ = os.Remove(details.FullOutputPath) })
			if details.Truncation.OutputBytes > 16*1024 || details.Truncation.OutputLines > 400 {
				t.Fatal("bash result exceeds history cap")
			}
			if strings.Contains(text, "omitted-head") || !strings.Contains(text, "final-error") {
				t.Fatal("bash result must retain the tail and omit the head")
			}
			if details.FullOutputPath == "" || !strings.Contains(text, "Full output: "+details.FullOutputPath) {
				t.Fatal("bash result must mention the full output temp file")
			}
			fullOutput, err := os.ReadFile(details.FullOutputPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(fullOutput) != tc.output {
				t.Fatal("temp file must preserve the complete bash output")
			}
		})
	}
}

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

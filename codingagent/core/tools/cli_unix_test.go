//go:build unix

package tools

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cappuch/maiku/agent"
)

func cliTestTools(t *testing.T) (*agent.AgentTool, *agent.AgentTool) {
	t.Helper()
	tools := CreateBuiltinTools(t.TempDir(), []string{"cli", "wait"})
	return tools[0], tools[1]
}

func cliCall(t *testing.T, tool *agent.AgentTool, params map[string]any) agent.AgentToolResult {
	t.Helper()
	result, err := tool.Execute(context.Background(), "test", params, nil)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func cliStart(t *testing.T, cli *agent.AgentTool, command string) string {
	t.Helper()
	r := cliCall(t, cli, map[string]any{"command": command, "yield_ms": 0})
	id := r.Details.(*CLIToolDetails).SessionID
	t.Cleanup(func() {
		r, _ := cli.Execute(context.Background(), "cleanup", map[string]any{"action": "terminate", "session_id": id, "yield_ms": 1000}, nil)
		if d, ok := r.Details.(*CLIToolDetails); ok {
			_ = os.Remove(d.FullOutputPath)
		}
	})
	return id
}

func TestCLIInteractiveInputAndIncrementalOutput(t *testing.T) {
	cli, wait := cliTestTools(t)
	id := cliStart(t, cli, "read first; echo first:$first; read second; echo second:$second")
	first := cliCall(t, cli, map[string]any{"action": "input", "session_id": id, "input": "hello\n", "yield_ms": 100})
	if !strings.Contains(first.Content[0].Text, "first:hello") || !first.Details.(*CLIToolDetails).Running {
		t.Fatalf("first input result = %+v", first)
	}
	second := cliCall(t, cli, map[string]any{"action": "input", "session_id": id, "input": "world\n", "yield_ms": 0})
	finished := cliCall(t, wait, map[string]any{"session_id": id, "seconds": 2})
	d := finished.Details.(*CLIToolDetails)
	if d.Running || d.ExitCode == nil || *d.ExitCode != 0 {
		t.Fatalf("completion = %+v", d)
	}
	// Input may return the second line immediately; no consumed output is replayed.
	if !strings.Contains(second.Content[0].Text+finished.Content[0].Text, "second:world") {
		t.Fatal("follow-up input output missing")
	}
	if strings.Contains(finished.Content[0].Text, "first:hello") {
		t.Fatal("wait replayed old output")
	}
	log, err := os.ReadFile(d.FullOutputPath)
	if err != nil || string(log) != "first:hello\nsecond:world\n" {
		t.Fatalf("full log = %q, %v", log, err)
	}
	poll := cliCall(t, cli, map[string]any{"action": "poll", "session_id": id, "yield_ms": 0})
	if strings.Contains(poll.Content[0].Text, "second:world") {
		t.Fatal("poll replayed output")
	}
}

func TestCLIOutputCapAndFullLog(t *testing.T) {
	cli, _ := cliTestTools(t)
	// One long line exercises byte truncation; many short lines exercise line truncation.
	for _, command := range []string{
		"printf head; printf '%20000s' x; printf '\\nfinal-error'",
		"echo head; i=0; while [ $i -lt 500 ]; do echo line; i=$((i+1)); done; echo final-error",
	} {
		r := cliCall(t, cli, map[string]any{"command": command, "yield_ms": 2000})
		d := r.Details.(*CLIToolDetails)
		t.Cleanup(func() { _ = os.Remove(d.FullOutputPath) })
		text := r.Content[0].Text
		if d.Running || strings.Contains(text, "head") || !strings.Contains(text, "final-error") || !strings.Contains(text, "Full output: "+d.FullOutputPath) {
			t.Fatalf("unexpected capped result: %q", text)
		}
		log, err := os.ReadFile(d.FullOutputPath)
		if err != nil || !strings.HasPrefix(string(log), "head") {
			t.Fatalf("full output missing: %v", err)
		}
		if len(text) > DefaultMaxBytes+512 {
			t.Fatal("output exceeds cap plus status notes")
		}
	}
}

func TestCLINonzeroExitEOFAndTermination(t *testing.T) {
	cli, wait := cliTestTools(t)
	r := cliCall(t, cli, map[string]any{"command": "exit 7", "yield_ms": 1000})
	d := r.Details.(*CLIToolDetails)
	defer os.Remove(d.FullOutputPath)
	if d.Running || d.ExitCode == nil || *d.ExitCode != 7 {
		t.Fatalf("exit status = %+v", d)
	}
	id := cliStart(t, cli, "cat")
	cliCall(t, cli, map[string]any{"action": "input", "session_id": id, "input": "EOF test\n", "close_stdin": true, "yield_ms": 0})
	r = cliCall(t, wait, map[string]any{"session_id": id, "seconds": 2})
	if r.Details.(*CLIToolDetails).Running {
		t.Fatal("EOF did not complete command")
	}
	id = cliStart(t, cli, "sleep 30")
	r = cliCall(t, cli, map[string]any{"action": "terminate", "session_id": id, "yield_ms": 1000})
	if r.Details.(*CLIToolDetails).Running {
		t.Fatal("termination did not stop command")
	}
}

func TestCLIWaitCancellationAndLifetime(t *testing.T) {
	cli, wait := cliTestTools(t)
	id := cliStart(t, cli, "sleep 30")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := wait.Execute(ctx, "test", map[string]any{"session_id": id, "seconds": 10}, nil); err != context.Canceled {
		t.Fatalf("cancellation error = %v", err)
	}
	r := cliCall(t, wait, map[string]any{"session_id": id, "seconds": 2})
	if r.Details.(*CLIToolDetails).Running {
		t.Fatal("cancelled wait left process running")
	}
	r = cliCall(t, cli, map[string]any{"command": "sleep 30", "timeout": .05, "yield_ms": 1000})
	d := r.Details.(*CLIToolDetails)
	defer os.Remove(d.FullOutputPath)
	if d.Running || !strings.Contains(r.Content[0].Text, "timed out") {
		t.Fatalf("timeout result = %+v", r)
	}
}

func TestWaitValidationAndAlias(t *testing.T) {
	tools := CreateBuiltinTools(t.TempDir(), []string{"wait_for"})
	wait := tools[0]
	if wait.Name != "wait_for" {
		t.Fatal("alias not registered")
	}
	for _, params := range []map[string]any{{}, {"seconds": -1}, {"seconds": 61}, {"session_id": "missing"}} {
		if _, err := wait.Execute(context.Background(), "test", params, nil); err == nil {
			t.Fatalf("accepted invalid wait: %v", params)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := wait.Execute(ctx, "test", map[string]any{"seconds": 10}, nil); err != context.DeadlineExceeded {
		t.Fatalf("pause cancellation = %v", err)
	}
	cliCall(t, wait, map[string]any{"seconds": 0})
}

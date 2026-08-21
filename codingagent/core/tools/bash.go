package tools

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/mikus/maiku/agent"
	"github.com/mikus/maiku/ai"
)

const (
	maxTimeoutMs      = 2_147_483_647
	maxTimeoutSeconds = maxTimeoutMs / 1000
)

var bashSchema = []byte(`{
	"type": "object",
	"properties": {
		"command": {"type": "string", "description": "Bash command to execute"},
		"timeout": {"type": "number", "description": "Timeout in seconds (optional, no default timeout)"}
	},
	"required": ["command"]
}`)

// BashToolDetails mirrors TS BashToolDetails.
type BashToolDetails struct {
	Truncation     *TruncationResult
	FullOutputPath string
}

func resolveTimeoutMs(timeoutSeconds *float64) (*time.Duration, error) {
	if timeoutSeconds == nil {
		return nil, nil
	}
	t := *timeoutSeconds
	if math.IsNaN(t) || math.IsInf(t, 0) || t <= 0 {
		return nil, errors.New("invalid timeout: must be a finite number of seconds")
	}
	timeoutMs := t * 1000
	if timeoutMs > maxTimeoutMs {
		return nil, fmt.Errorf("invalid timeout: maximum is %d seconds", maxTimeoutSeconds)
	}
	d := time.Duration(t * float64(time.Second))
	return &d, nil
}

// safeOutputBuffer is a mutex-guarded byte buffer used to capture
// interleaved stdout/stderr from the child process.
type safeOutputBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeOutputBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeOutputBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func writeFullOutputTempFile(content string) (string, error) {
	idBytes := make([]byte, 8)
	if _, err := rand.Read(idBytes); err != nil {
		return "", err
	}
	path := filepath.Join(os.TempDir(), fmt.Sprintf("pi-bash-%s.log", hex.EncodeToString(idBytes)))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func lastLineByteLength(content string) int {
	lines := splitLinesForCounting(content)
	if len(lines) == 0 {
		return 0
	}
	return len(lines[len(lines)-1])
}

func formatBashOutput(fullOutput string, emptyText string) (string, *BashToolDetails, error) {
	truncation := TruncateTail(fullOutput, nil)
	text := truncation.Content
	if text == "" {
		text = emptyText
	}
	var details *BashToolDetails
	if truncation.Truncated {
		fullOutputPath, err := writeFullOutputTempFile(fullOutput)
		if err != nil {
			return "", nil, err
		}
		details = &BashToolDetails{Truncation: &truncation, FullOutputPath: fullOutputPath}
		startLine := truncation.TotalLines - truncation.OutputLines + 1
		endLine := truncation.TotalLines
		switch {
		case truncation.LastLinePartial:
			lastLineSize := FormatSize(lastLineByteLength(fullOutput))
			text += fmt.Sprintf("\n\n[Showing last %s of line %d (line is %s). Full output: %s]", FormatSize(truncation.OutputBytes), endLine, lastLineSize, fullOutputPath)
		case truncation.TruncatedBy == TruncatedByLines:
			text += fmt.Sprintf("\n\n[Showing lines %d-%d of %d. Full output: %s]", startLine, endLine, truncation.TotalLines, fullOutputPath)
		default:
			text += fmt.Sprintf("\n\n[Showing lines %d-%d of %d (%s limit). Full output: %s]", startLine, endLine, truncation.TotalLines, FormatSize(DefaultMaxBytes), fullOutputPath)
		}
	}
	return text, details, nil
}

func appendStatus(text, status string) string {
	if text != "" {
		return text + "\n\n" + status
	}
	return status
}

// CreateBashTool ports coding-agent's bash tool (core/tools/bash.ts),
// EXECUTE logic only (TUI rendering omitted).
//
// Deviations from TS:
//   - No streaming tool_execution_update output while the command runs
//     (onUpdate is never called); the result is only produced once the
//     command finishes. TS throttles/streams partial output for the TUI.
//   - No PI_SESSION_ID/PI_PROVIDER/PI_MODEL/etc environment injection, since
//     AgentTool.Execute has no ExtensionContext (session/model) available.
//   - Full output is buffered in memory during execution and only written
//     to a temp file after the command completes (if truncated), instead of
//     streaming writes to disk as output arrives.
//   - Process tree killing uses a POSIX process group (setpgid + kill on the
//     negative pgid); Windows is not supported.
func CreateBashTool(cwd string) *agent.AgentTool {
	return &agent.AgentTool{
		Tool: ai.Tool{
			Name: "bash",
			Description: fmt.Sprintf(
				"Execute a bash command in the current working directory. Returns stdout and stderr. Output is truncated to last %d lines or %dKB (whichever is hit first). If truncated, full output is saved to a temp file. Optionally provide a timeout in seconds.",
				DefaultMaxLines, DefaultMaxBytes/1024,
			),
			Parameters: bashSchema,
		},
		Label: "bash",
		Execute: func(ctx context.Context, _ string, params map[string]any, _ agent.AgentToolUpdateCallback) (agent.AgentToolResult, error) {
			command, _ := argString(params, "command")
			timeoutSeconds := argNumberPtr(params, "timeout")

			timeoutDuration, err := resolveTimeoutMs(timeoutSeconds)
			if err != nil {
				return agent.AgentToolResult{}, err
			}

			if err := checkAborted(ctx); err != nil {
				return agent.AgentToolResult{}, err
			}
			if _, statErr := os.Stat(cwd); statErr != nil {
				return agent.AgentToolResult{}, fmt.Errorf("working directory does not exist: %s; cannot execute bash commands", cwd)
			}

			runCtx := ctx
			var cancelTimeout context.CancelFunc
			timedOut := false
			if timeoutDuration != nil {
				runCtx, cancelTimeout = context.WithTimeout(ctx, *timeoutDuration)
				defer cancelTimeout()
			}

			cmd := exec.Command("sh", "-c", command)
			cmd.Dir = cwd
			cmd.Env = os.Environ()
			configureBashCmd(cmd)

			out := &safeOutputBuffer{}
			cmd.Stdout = out
			cmd.Stderr = out

			if startErr := cmd.Start(); startErr != nil {
				return agent.AgentToolResult{}, startErr
			}

			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()

			var waitErr error
			select {
			case waitErr = <-done:
			case <-runCtx.Done():
				killProcessGroup(cmd)
				waitErr = <-done
				if timeoutDuration != nil && ctx.Err() == nil {
					timedOut = true
				}
			}

			aborted := ctx.Err() != nil && !timedOut

			fullOutput := out.String()

			if aborted {
				text, _, ferr := formatBashOutput(fullOutput, "")
				if ferr != nil {
					return agent.AgentToolResult{}, ferr
				}
				return agent.AgentToolResult{}, errors.New(appendStatus(text, "Command aborted"))
			}
			if timedOut {
				text, _, ferr := formatBashOutput(fullOutput, "")
				if ferr != nil {
					return agent.AgentToolResult{}, ferr
				}
				secs := "?"
				if timeoutSeconds != nil {
					secs = fmt.Sprintf("%g", *timeoutSeconds)
				}
				return agent.AgentToolResult{}, errors.New(appendStatus(text, fmt.Sprintf("Command timed out after %s seconds", secs)))
			}

			outputText, details, ferr := formatBashOutput(fullOutput, "(no output)")
			if ferr != nil {
				return agent.AgentToolResult{}, ferr
			}

			exitCode := 0
			if waitErr != nil {
				if exitErr, ok := errors.AsType[*exec.ExitError](waitErr); ok {
					exitCode = exitErr.ExitCode()
				} else {
					return agent.AgentToolResult{}, waitErr
				}
			}

			if exitCode != 0 {
				return agent.AgentToolResult{}, errors.New(appendStatus(outputText, fmt.Sprintf("Command exited with code %d", exitCode)))
			}

			result := agent.AgentToolResult{
				Content: []ai.ToolResultContent{{Type: "text", Text: outputText}},
			}
			if details != nil {
				result.Details = details
			}
			return result, nil
		},
	}
}

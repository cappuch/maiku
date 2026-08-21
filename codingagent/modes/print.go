package modes

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync/atomic"

	"github.com/mikus/maiku/agent"
	"github.com/mikus/maiku/ai"
	"github.com/mikus/maiku/codingagent/core"
)

// PrintModeOptions configures a single-shot run.
type PrintModeOptions struct {
	// Mode is "text" (final assistant text only) or "json" (event stream).
	Mode string
	// InitialMessage is the first prompt; it may already include @file content.
	InitialMessage string
	// InitialImages are attached to the initial prompt.
	InitialImages []ai.ImageContent
	// Messages are additional prompts sent one after another.
	Messages []string
	// Stdout defaults to os.Stdout, Stderr to os.Stderr.
	Stdout io.Writer
	Stderr io.Writer
}

// RunPrintMode sends the prompts, writes output, and returns a process exit
// code. It never returns before the agent has gone idle.
func RunPrintMode(ctx context.Context, session *core.AgentSession, options PrintModeOptions) int {
	stdout := options.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := options.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	jsonMode := options.Mode == "json"
	writer := bufio.NewWriter(stdout)
	var outputFailed atomic.Bool
	finish := func(code int) int {
		if err := writer.Flush(); err != nil {
			outputFailed.Store(true)
		}
		if outputFailed.Load() {
			return 1
		}
		return code
	}

	defer session.Dispose()

	if jsonMode {
		if manager := session.SessionManager(); manager != nil {
			if header, err := json.Marshal(manager.Header()); err == nil {
				if _, err := fmt.Fprintf(writer, "%s\n", header); err != nil {
					outputFailed.Store(true)
				}
				if err := writer.Flush(); err != nil {
					outputFailed.Store(true)
				}
			}
		}
		if outputFailed.Load() {
			return finish(1)
		}

		unsubscribe := session.Subscribe(func(event agent.AgentEvent) {
			line, err := json.Marshal(ToJSONEvent(event))
			if err != nil {
				return
			}
			if _, err := fmt.Fprintf(writer, "%s\n", line); err != nil {
				outputFailed.Store(true)
				return
			}
			// Flush per event so consumers can stream the output.
			if err := writer.Flush(); err != nil {
				outputFailed.Store(true)
			}
		})
		defer unsubscribe()
	}

	prompts := make([]string, 0, len(options.Messages)+1)
	if options.InitialMessage != "" {
		prompts = append(prompts, options.InitialMessage)
	}
	prompts = append(prompts, options.Messages...)

	if len(prompts) == 0 {
		if _, err := fmt.Fprintln(stderr, "No prompt provided. Pass a message, e.g. pi -p \"list the go files\""); err != nil {
			return finish(1)
		}
		return finish(1)
	}

	for i, prompt := range prompts {
		var err error
		if i == 0 {
			err = session.Prompt(ctx, prompt, options.InitialImages...)
		} else {
			err = session.Prompt(ctx, prompt)
		}
		if err != nil {
			if _, writeErr := fmt.Fprintln(stderr, err.Error()); writeErr != nil {
				return finish(1)
			}
			return finish(1)
		}
		if outputFailed.Load() {
			return finish(1)
		}
	}

	if jsonMode {
		return finish(exitCodeFromLastMessage(session))
	}

	assistant, ok := session.LastAssistantMessage()
	if !ok {
		return finish(0)
	}
	if assistant.StopReason == ai.StopError || assistant.StopReason == ai.StopAborted {
		message := assistant.ErrorMessage
		if message == "" {
			message = fmt.Sprintf("Request %s", assistant.StopReason)
		}
		if _, err := fmt.Fprintln(stderr, message); err != nil {
			return finish(1)
		}
		return finish(1)
	}

	for _, block := range assistant.Content {
		if block.Type == "text" {
			if _, err := fmt.Fprintf(writer, "%s\n", block.Text); err != nil {
				outputFailed.Store(true)
				break
			}
		}
	}
	return finish(0)
}

func exitCodeFromLastMessage(session *core.AgentSession) int {
	assistant, ok := session.LastAssistantMessage()
	if !ok {
		return 0
	}
	if assistant.StopReason == ai.StopError || assistant.StopReason == ai.StopAborted {
		return 1
	}
	return 0
}

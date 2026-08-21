package core

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mikus/maiku/agent"
	"github.com/mikus/maiku/ai"
)

func testSubagentModel() ai.Model {
	return ai.Model{
		ID: "test-model", Name: "test-model", API: "test-api", Provider: "test-provider",
		Input: []string{"text"}, ContextWindow: 100_000, MaxTokens: 4_096,
	}
}

func completedSubagentStream(text string, inspect func(ai.Context)) agent.StreamFn {
	return func(model ai.Model, ctx ai.Context, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		if inspect != nil {
			inspect(ctx)
		}
		stream := ai.NewAssistantMessageEventStream()
		message := ai.AssistantMessage{
			Role:       "assistant",
			Content:    []ai.AssistantContentBlock{ai.TextBlock(text)},
			API:        model.API,
			Provider:   model.Provider,
			Model:      model.ID,
			Usage:      ai.Usage{Input: 10, Output: 5, TotalTokens: 15},
			StopReason: ai.StopStop,
		}
		stream.Push(ai.AssistantMessageEvent{Type: "done", Reason: ai.StopStop, Message: &message})
		return stream
	}
}

func toolNames(tools []agent.AgentTool) map[string]bool {
	out := make(map[string]bool, len(tools))
	for _, tool := range tools {
		out[tool.Name] = true
	}
	return out
}

func TestSelectRootToolsRegistersSubagentOnlyOnRoot(t *testing.T) {
	runner := NewSubagentRunner(SubagentToolOptions{Cwd: t.TempDir(), Model: testSubagentModel()})

	rootNames := toolNames(SelectRootTools(t.TempDir(), nil, nil, false, runner))
	if !rootNames[SubagentToolName] {
		t.Fatal("root toolset does not contain subagent")
	}
	childNames := toolNames(SelectTools(t.TempDir(), nil, nil, false))
	if childNames[SubagentToolName] {
		t.Fatal("normal/child toolset unexpectedly contains subagent")
	}

	onlySubagent := SelectRootTools(t.TempDir(), []string{SubagentToolName}, nil, false, runner)
	if len(onlySubagent) != 1 || onlySubagent[0].Name != SubagentToolName {
		t.Fatalf("explicit subagent allowlist selected %+v", toolNames(onlySubagent))
	}
	if got := SelectRootTools(t.TempDir(), nil, []string{SubagentToolName}, false, runner); toolNames(got)[SubagentToolName] {
		t.Fatal("subagent denylist was ignored")
	}
	if got := SelectRootTools(t.TempDir(), nil, nil, true, runner); len(got) != 0 {
		t.Fatalf("disableAll returned %d tools", len(got))
	}

	prompt := BuildSystemPrompt(BuildSystemPromptOptions{
		SelectedTools: []string{"read", SubagentToolName}, Cwd: t.TempDir(),
	})
	if !strings.Contains(prompt, "- subagent:") || !strings.Contains(prompt, "Issue multiple subagent calls") {
		t.Fatalf("root system prompt does not advertise orchestration behavior:\n%s", prompt)
	}
}

func TestSubagentRunsIndependentChildAndReturnsStructuredReport(t *testing.T) {
	cwd := t.TempDir()
	var inspected bool
	runner := NewSubagentRunner(SubagentToolOptions{
		Cwd:      cwd,
		AgentDir: t.TempDir(),
		Model:    testSubagentModel(),
		APIKey:   "test-key",
		StreamFn: completedSubagentStream("Implemented the requested check.", func(ctx ai.Context) {
			inspected = true
			got := make(map[string]bool, len(ctx.Tools))
			for _, tool := range ctx.Tools {
				got[tool.Name] = true
			}
			for _, name := range []string{"read", "bash", "edit", "write"} {
				if !got[name] {
					t.Errorf("child is missing normal tool %q", name)
				}
			}
			if got[SubagentToolName] {
				t.Error("child received recursive subagent tool")
			}
			if !strings.Contains(ctx.SystemPrompt, "You are a child Maiku agent") {
				t.Error("child report/non-recursion instructions missing from system prompt")
			}
		}),
	})

	tool := runner.Tool()
	if _, err := ai.ValidateToolArguments(tool.Tool, map[string]any{"task": "Inspect the implementation"}); err != nil {
		t.Fatalf("subagent schema rejected task: %v", err)
	}
	result, err := tool.Execute(context.Background(), "call-1", map[string]any{"task": "Inspect the implementation"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !inspected {
		t.Fatal("child stream was not invoked")
	}
	if runner.ActiveCount() != 0 {
		t.Fatalf("runner retained %d completed children", runner.ActiveCount())
	}
	if len(result.Content) != 1 {
		t.Fatalf("got %d result blocks", len(result.Content))
	}
	report := result.Content[0].Text
	for _, heading := range []string{
		"## Subagent report",
		"### Investigated or implemented",
		"### Files and actions",
		"### Findings and decisions",
		"### Errors or unresolved issues",
		"### Handoff to root",
	} {
		if !strings.Contains(report, heading) {
			t.Errorf("report missing %q:\n%s", heading, report)
		}
	}
	if result.Usage == nil || result.Usage.TotalTokens != 15 {
		t.Fatalf("child usage was not collected: %+v", result.Usage)
	}
	details, ok := result.Details.(SubagentToolDetails)
	if !ok || details.ID != "call-1" || details.Status != "completed" {
		t.Fatalf("unexpected details: %#v", result.Details)
	}
}

func TestSubagentRunnerAllowsConcurrentChildren(t *testing.T) {
	var active atomic.Int32
	var maximum atomic.Int32
	var started atomic.Int32
	release := make(chan struct{})
	var releaseOnce sync.Once

	streamFn := func(model ai.Model, _ ai.Context, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			old := maximum.Load()
			if current <= old || maximum.CompareAndSwap(old, current) {
				break
			}
		}
		if started.Add(1) == 2 {
			releaseOnce.Do(func() { close(release) })
		}
		select {
		case <-release:
		case <-time.After(2 * time.Second):
		}
		return completedSubagentStream("Concurrent task complete.", nil)(model, ai.Context{}, nil)
	}

	runner := NewSubagentRunner(SubagentToolOptions{
		Cwd: t.TempDir(), AgentDir: t.TempDir(), Model: testSubagentModel(), APIKey: "test-key", StreamFn: streamFn,
	})
	tool := runner.Tool()
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, err := tool.Execute(context.Background(), "", map[string]any{"task": "independent task"}, nil)
			errs <- err
		}()
	}
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if maximum.Load() < 2 {
		t.Fatalf("children did not overlap; maximum active streams = %d", maximum.Load())
	}
	if runner.ActiveCount() != 0 {
		t.Fatalf("runner retained %d completed children", runner.ActiveCount())
	}
}

func TestSubagentFailureReturnsMarkdownHandoff(t *testing.T) {
	failureStream := func(model ai.Model, _ ai.Context, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		stream := ai.NewAssistantMessageEventStream()
		message := ai.AssistantMessage{
			Role: "assistant", API: model.API, Provider: model.Provider, Model: model.ID,
			StopReason: ai.StopError, ErrorMessage: "provider unavailable",
		}
		stream.Push(ai.AssistantMessageEvent{Type: "error", Reason: ai.StopError, Error: &message})
		return stream
	}
	runner := NewSubagentRunner(SubagentToolOptions{
		Cwd: t.TempDir(), AgentDir: t.TempDir(), Model: testSubagentModel(), APIKey: "test-key", StreamFn: failureStream,
	})
	_, err := runner.Tool().Execute(context.Background(), "failed-call", map[string]any{"task": "do work"}, nil)
	if err == nil {
		t.Fatal("expected child failure")
	}
	message := err.Error()
	if !strings.HasPrefix(message, "## Subagent report") || !strings.Contains(message, "provider unavailable") || !strings.Contains(message, "### Handoff to root") {
		t.Fatalf("failure was not a useful Markdown report:\n%s", message)
	}
	if runner.ActiveCount() != 0 {
		t.Fatalf("failed child remained active: %d", runner.ActiveCount())
	}
}

func TestRootAgentExecutesMultipleSubagentCallsConcurrently(t *testing.T) {
	var childActive atomic.Int32
	var childMaximum atomic.Int32
	var childStarted atomic.Int32
	release := make(chan struct{})
	var releaseOnce sync.Once

	childStream := func(model ai.Model, _ ai.Context, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		current := childActive.Add(1)
		defer childActive.Add(-1)
		for {
			old := childMaximum.Load()
			if current <= old || childMaximum.CompareAndSwap(old, current) {
				break
			}
		}
		if childStarted.Add(1) == 2 {
			releaseOnce.Do(func() { close(release) })
		}
		select {
		case <-release:
		case <-time.After(2 * time.Second):
		}
		return completedSubagentStream("## Subagent report\n\n### Investigated or implemented\nDone.\n\n### Files and actions\nNone.\n\n### Findings and decisions\nDone.\n\n### Errors or unresolved issues\nNone.\n\n### Handoff to root\nContinue.", nil)(model, ai.Context{}, nil)
	}

	runner := NewSubagentRunner(SubagentToolOptions{
		Cwd: t.TempDir(), AgentDir: t.TempDir(), Model: testSubagentModel(), APIKey: "test-key", StreamFn: childStream,
	})
	var rootCalls atomic.Int32
	rootStream := func(model ai.Model, ctx ai.Context, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		stream := ai.NewAssistantMessageEventStream()
		var message ai.AssistantMessage
		if rootCalls.Add(1) == 1 {
			message = ai.AssistantMessage{
				Role: "assistant", API: model.API, Provider: model.Provider, Model: model.ID,
				Content: []ai.AssistantContentBlock{
					ai.ToolCallBlock(ai.ToolCall{Type: "toolCall", ID: "sub-1", Name: SubagentToolName, Arguments: map[string]any{"task": "first"}}),
					ai.ToolCallBlock(ai.ToolCall{Type: "toolCall", ID: "sub-2", Name: SubagentToolName, Arguments: map[string]any{"task": "second"}}),
				},
				StopReason: ai.StopToolUse,
			}
		} else {
			if len(ctx.Messages) < 4 || ctx.Messages[len(ctx.Messages)-1].Role != "toolResult" {
				t.Errorf("root did not receive child reports before continuing: %+v", ctx.Messages)
			}
			message = ai.AssistantMessage{
				Role: "assistant", API: model.API, Provider: model.Provider, Model: model.ID,
				Content: []ai.AssistantContentBlock{ai.TextBlock("Root synthesized the reports.")}, StopReason: ai.StopStop,
			}
		}
		stream.Push(ai.AssistantMessageEvent{Type: "done", Reason: message.StopReason, Message: &message})
		return stream
	}

	root := NewAgentSession(AgentSessionOptions{
		Model: testSubagentModel(), APIKey: "test-key", StreamFn: rootStream,
		SystemPrompt: "root", Tools: []agent.AgentTool{runner.Tool()},
	})
	defer root.Dispose()
	if err := root.Prompt(context.Background(), "delegate both tasks"); err != nil {
		t.Fatal(err)
	}
	if childMaximum.Load() < 2 {
		t.Fatalf("root agent loop did not execute subagents concurrently; max=%d", childMaximum.Load())
	}
	if got := root.LastAssistantText(); got != "Root synthesized the reports." {
		t.Fatalf("unexpected root result %q", got)
	}
}

func TestSubagentCancellationStopsChildAndCleansLifecycle(t *testing.T) {
	started := make(chan struct{})
	var startedOnce sync.Once
	blockingStream := func(model ai.Model, _ ai.Context, opts *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		stream := ai.NewAssistantMessageEventStream()
		startedOnce.Do(func() { close(started) })
		go func() {
			if opts.Signal != nil {
				<-opts.Signal
			}
			message := ai.AssistantMessage{
				Role: "assistant", API: model.API, Provider: model.Provider, Model: model.ID,
				StopReason: ai.StopAborted, ErrorMessage: "operation canceled",
			}
			stream.Push(ai.AssistantMessageEvent{Type: "error", Reason: ai.StopAborted, Error: &message})
		}()
		return stream
	}

	runner := NewSubagentRunner(SubagentToolOptions{
		Cwd: t.TempDir(), AgentDir: t.TempDir(), Model: testSubagentModel(), APIKey: "test-key", StreamFn: blockingStream,
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := runner.Tool().Execute(ctx, "cancel-call", map[string]any{"task": "wait"}, nil)
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("child did not start")
	}
	if runner.ActiveCount() != 1 {
		t.Fatalf("active children before cancel = %d, want 1", runner.ActiveCount())
	}
	cancel()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "operation canceled") {
			t.Fatalf("unexpected cancellation result: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("child did not stop after cancellation")
	}
	if runner.ActiveCount() != 0 {
		t.Fatalf("canceled child remained active: %d", runner.ActiveCount())
	}
}

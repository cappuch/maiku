package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sync/atomic"
	"testing"

	"github.com/cappuch/maiku/agent"
	"github.com/cappuch/maiku/ai"
)

// TestMemoryMockAgentFlows runs the real agent loop and real file tools
// against an emulated model. The stub stream returns scripted tool calls, so
// heap numbers are the harness, transcripts, and tool output — not an API.
func TestMemoryMockAgentFlows(t *testing.T) {
	cwd := t.TempDir()
	payload := []byte(repeatLines("package p\nfunc F() int { return 1 }\n", 40*1024))
	if err := os.WriteFile(filepath.Join(cwd, "src.go"), payload, 0o644); err != nil {
		t.Fatal(err)
	}

	base := settled()
	fmt.Printf("\nbaseline settled heap %s\n", kb(base.HeapAlloc))

	beforeSolo := settled()
	solo := runSolo(t, cwd, 15)
	fmt.Printf("solo 15 reads: peak live %s, settled after dispose %s, transcript %s across %d messages, alloc during flow %s\n",
		kb(solo.peak), kb(solo.settled), kbInt(solo.transcript), solo.messages, kb(solo.totalAlloc-beforeSolo.TotalAlloc))

	beforeSwarm := settled()
	swarm := runSwarm(t, cwd, 8, 6)
	fmt.Printf("swarm 8 children x 6 reads: peak live %s (children in flight %d), settled after dispose %s, root transcript %s across %d messages, alloc during flow %s\n",
		kb(swarm.peak), swarm.children, kb(swarm.settled), kbInt(swarm.transcript), swarm.messages, kb(swarm.totalAlloc-beforeSwarm.TotalAlloc))

	beforeLight := settled()
	light := runSwarm(t, cwd, 8, 0)
	fmt.Printf("swarm 8 children x report only: peak live %s (children in flight %d), settled after dispose %s, root transcript %s across %d messages, alloc during flow %s\n",
		kb(light.peak), light.children, kb(light.settled), kbInt(light.transcript), light.messages, kb(light.totalAlloc-beforeLight.TotalAlloc))

	if solo.peak == 0 || swarm.peak == 0 {
		t.Fatal("peak heap was not sampled")
	}
	if swarm.children != 8 {
		t.Fatalf("swarm did not hold 8 children at once: %d", swarm.children)
	}
}

type flowResult struct {
	peak       uint64
	settled    uint64
	totalAlloc uint64
	transcript int
	messages   int
	children   int
}

func runSolo(t *testing.T, cwd string, reads int) flowResult {
	t.Helper()
	var calls atomic.Int32
	var peak atomic.Uint64
	stream := func(model ai.Model, _ ai.Context, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		n := int(calls.Add(1))
		var message ai.AssistantMessage
		if n <= reads {
			message = toolMessage(model, "read", fmt.Sprintf("read-%d", n), map[string]any{"path": "src.go"})
		} else {
			message = textMessage(model, "done")
			peak.Store(liveHeap())
		}
		return doneStream(message)
	}
	tools := SelectToolsWithOptions(cwd, []string{"read"}, nil, false, ToolOptions{})
	session := NewAgentSession(AgentSessionOptions{
		Model: testSubagentModel(), APIKey: "test-key", StreamFn: stream,
		SystemPrompt: "solo", Tools: tools, SessionID: "solo",
	})
	if err := session.Prompt(context.Background(), "read the file until you understand it"); err != nil {
		t.Fatal(err)
	}
	transcript, messages := transcriptSize(session.State().Messages)
	session.Dispose()
	session = nil
	settled := settled()
	return flowResult{
		peak: peak.Load(), settled: settled.HeapAlloc, totalAlloc: settled.TotalAlloc,
		transcript: transcript, messages: messages,
	}
}

func runSwarm(t *testing.T, cwd string, children, readsEach int) flowResult {
	t.Helper()
	release := make(chan struct{})
	var ready atomic.Int32
	var maxLive atomic.Int32
	var live atomic.Int32
	var peak atomic.Uint64

	childTurns := func(model ai.Model, ctx ai.Context, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		// User + assistant + tool result per completed read, plus the new user turn.
		readsDone := 0
		for _, m := range ctx.Messages {
			if m.Role == "toolResult" {
				readsDone++
			}
		}
		if readsDone < readsEach {
			return doneStream(toolMessage(model, "read", fmt.Sprintf("read-%d", readsDone+1), map[string]any{"path": "src.go"}))
		}
		now := int(live.Add(1))
		for {
			old := maxLive.Load()
			if int32(now) <= old || maxLive.CompareAndSwap(old, int32(now)) {
				break
			}
		}
		if ready.Add(1) == int32(children) {
			peak.Store(liveHeap())
			close(release)
		}
		<-release
		live.Add(-1)
		return doneStream(textMessage(model, "## Subagent report\n\n### Investigated or implemented\nRead src.go.\n\n### Files and actions\nread src.go\n\n### Findings and decisions\nSmall function.\n\n### Errors or unresolved issues\nNone.\n\n### Handoff to root\nDone."))
	}

	runner := NewSubagentRunner(SubagentToolOptions{
		Cwd: cwd, AgentDir: t.TempDir(), Model: testSubagentModel(), APIKey: "test-key",
		StreamFn:   childTurns,
		ChildTools: SelectToolsWithOptions(cwd, []string{"read"}, nil, false, ToolOptions{}),
	})

	var rootCalls atomic.Int32
	rootStream := func(model ai.Model, _ ai.Context, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		if rootCalls.Add(1) == 1 {
			blocks := make([]ai.AssistantContentBlock, 0, children)
			for i := 1; i <= children; i++ {
				blocks = append(blocks, ai.ToolCallBlock(ai.ToolCall{
					Type: "toolCall", ID: fmt.Sprintf("sub-%d", i), Name: SubagentToolName,
					Arguments: map[string]any{"task": fmt.Sprintf("inspect copy %d of src.go", i)},
				}))
			}
			return doneStream(ai.AssistantMessage{
				Role: "assistant", API: model.API, Provider: model.Provider, Model: model.ID,
				Content: blocks, StopReason: ai.StopToolUse, Usage: ai.Usage{Input: 100, Output: 40, TotalTokens: 140},
			})
		}
		return doneStream(textMessage(model, "Root folded the reports."))
	}

	root := NewAgentSession(AgentSessionOptions{
		Model: testSubagentModel(), APIKey: "test-key", StreamFn: rootStream,
		SystemPrompt: "root", Tools: []agent.AgentTool{runner.Tool()}, SessionID: "swarm",
	})
	if err := root.Prompt(context.Background(), "fan out"); err != nil {
		t.Fatal(err)
	}
	transcript, messages := transcriptSize(root.State().Messages)
	root.Dispose()
	settledHeap := settled()
	return flowResult{
		peak: peak.Load(), settled: settledHeap.HeapAlloc, totalAlloc: settledHeap.TotalAlloc,
		transcript: transcript, messages: messages, children: int(maxLive.Load()),
	}
}

func toolMessage(model ai.Model, name, id string, args map[string]any) ai.AssistantMessage {
	return ai.AssistantMessage{
		Role: "assistant", API: model.API, Provider: model.Provider, Model: model.ID,
		Content:    []ai.AssistantContentBlock{ai.ToolCallBlock(ai.ToolCall{Type: "toolCall", ID: id, Name: name, Arguments: args})},
		StopReason: ai.StopToolUse,
		Usage:      ai.Usage{Input: 100, Output: 20, TotalTokens: 120},
	}
}

func textMessage(model ai.Model, text string) ai.AssistantMessage {
	return ai.AssistantMessage{
		Role: "assistant", API: model.API, Provider: model.Provider, Model: model.ID,
		Content: []ai.AssistantContentBlock{ai.TextBlock(text)}, StopReason: ai.StopStop,
		Usage: ai.Usage{Input: 100, Output: 30, TotalTokens: 130},
	}
}

func doneStream(message ai.AssistantMessage) *ai.AssistantMessageEventStream {
	stream := ai.NewAssistantMessageEventStream()
	stream.Push(ai.AssistantMessageEvent{Type: "done", Reason: message.StopReason, Message: &message})
	return stream
}

func transcriptSize(messages []ai.Message) (int, int) {
	n := 0
	for _, m := range messages {
		switch m.Role {
		case "user":
			if text, ok := m.UserContent.(string); ok {
				n += len(text)
			}
			if blocks, ok := m.UserContent.([]any); ok {
				for _, b := range blocks {
					if t, ok := b.(ai.TextContent); ok {
						n += len(t.Text)
					}
				}
			}
		case "assistant":
			for _, b := range m.AssistantContent {
				n += len(b.Text) + len(b.Thinking)
				if b.Arguments != nil {
					n += 64
				}
			}
		case "toolResult":
			for _, b := range m.ToolContent {
				n += len(b.Text)
			}
		}
	}
	return n, len(messages)
}

func liveHeap() uint64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

func settled() runtime.MemStats {
	runtime.GC()
	debug.FreeOSMemory()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m
}

func kb(n uint64) string {
	return fmt.Sprintf("%.1f KiB", float64(n)/1024)
}

func kbInt(n int) string { return kb(uint64(n)) }

func repeatLines(line string, size int) string {
	out := make([]byte, 0, size)
	for len(out)+len(line) <= size {
		out = append(out, line...)
	}
	return string(out)
}

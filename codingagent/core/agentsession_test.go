package core

import (
	"testing"

	"github.com/mikus/maiku/agent"
	"github.com/mikus/maiku/ai"
)

func TestConvertToLLMDropsThinkingOnlyAssistant(t *testing.T) {
	in := []agent.AgentMessage{
		{Role: "user", UserContent: "hi"},
		{
			Role: "assistant",
			AssistantContent: []ai.AssistantContentBlock{
				{Type: "thinking", Thinking: "only thoughts"},
			},
			StopReason: ai.StopLength,
		},
		{Role: "user", UserContent: "continue"},
		{
			Role:       "assistant",
			StopReason: ai.StopError,
			ErrorMessage: "provider API error",
		},
		{
			Role: "assistant",
			AssistantContent: []ai.AssistantContentBlock{
				{Type: "text", Text: "ok"},
			},
			StopReason: ai.StopStop,
		},
	}
	out, err := ConvertToLLM(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 {
		t.Fatalf("got %d messages, want 3 (user, user, assistant text)", len(out))
	}
	if out[0].Role != "user" || out[1].Role != "user" || out[2].Role != "assistant" {
		t.Fatalf("unexpected roles: %+v", []string{out[0].Role, out[1].Role, out[2].Role})
	}
}

func TestConvertToLLMKeepsToolCalls(t *testing.T) {
	in := []agent.AgentMessage{
		{Role: "user", UserContent: "run"},
		{
			Role: "assistant",
			AssistantContent: []ai.AssistantContentBlock{
				{Type: "thinking", Thinking: "plan"},
				{Type: "toolCall", ID: "1", Name: "bash", Arguments: map[string]any{"command": "ls"}},
			},
			StopReason: ai.StopToolUse,
		},
		{Role: "toolResult", ToolCallID: "1", ToolName: "bash", ToolContent: []ai.ToolResultContent{{Type: "text", Text: "ok"}}},
	}
	out, err := ConvertToLLM(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 {
		t.Fatalf("got %d messages, want 3", len(out))
	}
}

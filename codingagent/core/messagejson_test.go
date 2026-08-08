package core

import (
	"testing"

	"github.com/mikus/maiku/ai"
)

func TestEncodeDecodeAssistantMessage(t *testing.T) {
	usage := ai.EmptyUsage()
	original := ai.Message{
		Role: "assistant",
		AssistantContent: []ai.AssistantContentBlock{
			{Type: "text", Text: "hello"},
			{Type: "toolCall", ID: "call_1", Name: "read", Arguments: map[string]any{"path": "main.go"}},
		},
		API:        "anthropic-messages",
		Provider:   "anthropic",
		Model:      "claude-sonnet-4-5",
		Usage:      &usage,
		StopReason: ai.StopToolUse,
		Timestamp:  42,
	}

	encoded, err := EncodeMessage(original)
	if err != nil {
		t.Fatalf("EncodeMessage: %v", err)
	}
	decoded, err := DecodeMessage(encoded)
	if err != nil {
		t.Fatalf("DecodeMessage: %v", err)
	}

	if len(decoded.AssistantContent) != 2 {
		t.Fatalf("got %d content blocks, want 2", len(decoded.AssistantContent))
	}
	if decoded.AssistantContent[0].Text != "hello" {
		t.Errorf("text block = %q, want %q", decoded.AssistantContent[0].Text, "hello")
	}
	if decoded.AssistantContent[1].Name != "read" {
		t.Errorf("tool call name = %q, want %q", decoded.AssistantContent[1].Name, "read")
	}
	if decoded.StopReason != ai.StopToolUse || decoded.Model != "claude-sonnet-4-5" {
		t.Errorf("metadata lost: %+v", decoded)
	}
}

func TestEncodeDecodeUserAndToolResult(t *testing.T) {
	user := ai.Message{
		Role:        "user",
		UserContent: []any{ai.TextContent{Type: "text", Text: "hi"}},
		Timestamp:   1,
	}
	encoded, err := EncodeMessage(user)
	if err != nil {
		t.Fatalf("EncodeMessage(user): %v", err)
	}
	decoded, err := DecodeMessage(encoded)
	if err != nil {
		t.Fatalf("DecodeMessage(user): %v", err)
	}
	if got := ai.ContentText(decoded.UserContent); got != "hi" {
		t.Errorf("user text = %q, want %q", got, "hi")
	}

	toolResult := ai.Message{
		Role:        "toolResult",
		ToolCallID:  "call_1",
		ToolName:    "read",
		ToolContent: []ai.ToolResultContent{{Type: "text", Text: "file body"}},
		Timestamp:   2,
	}
	encoded, err = EncodeMessage(toolResult)
	if err != nil {
		t.Fatalf("EncodeMessage(toolResult): %v", err)
	}
	decoded, err = DecodeMessage(encoded)
	if err != nil {
		t.Fatalf("DecodeMessage(toolResult): %v", err)
	}
	if len(decoded.ToolContent) != 1 || decoded.ToolContent[0].Text != "file body" {
		t.Errorf("tool result content lost: %+v", decoded.ToolContent)
	}
	if decoded.ToolCallID != "call_1" || decoded.ToolName != "read" {
		t.Errorf("tool result metadata lost: %+v", decoded)
	}
}

package compaction

import (
	"context"
	"strings"
	"testing"

	"github.com/mikus/maiku/agent"
	"github.com/mikus/maiku/ai"
)

func userMessage(text string) agent.AgentMessage {
	return agent.AgentMessage{Role: "user", UserContent: []any{ai.TextContent{Type: "text", Text: text}}}
}

func assistantMessage(text string, totalTokens int) agent.AgentMessage {
	message := agent.AgentMessage{
		Role:             "assistant",
		AssistantContent: []ai.AssistantContentBlock{ai.TextBlock(text)},
		StopReason:       ai.StopStop,
	}
	if totalTokens > 0 {
		message.Usage = &ai.Usage{TotalTokens: totalTokens}
	}
	return message
}

func TestShouldCompact(t *testing.T) {
	settings := Settings{Enabled: true, ReserveTokens: 16384, KeepRecentTokens: 20000}

	tests := []struct {
		name          string
		contextTokens int
		contextWindow int
		settings      Settings
		want          bool
	}{
		{"below threshold", 100_000, 200_000, settings, false},
		{"just below threshold", 183_616, 200_000, settings, false},
		{"just above threshold", 183_617, 200_000, settings, true},
		{"disabled", 199_000, 200_000, Settings{Enabled: false, ReserveTokens: 16384}, false},
		{"unknown context window", 199_000, 0, settings, false},
		{"zero reserve falls back to default", 190_000, 200_000, Settings{Enabled: true}, true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ShouldCompact(test.contextTokens, test.contextWindow, test.settings); got != test.want {
				t.Errorf("ShouldCompact(%d, %d) = %v, want %v",
					test.contextTokens, test.contextWindow, got, test.want)
			}
		})
	}
}

func TestCalculateContextTokensFallsBackToComponents(t *testing.T) {
	if got := CalculateContextTokens(ai.Usage{TotalTokens: 42, Input: 1}); got != 42 {
		t.Errorf("got %d, want the reported total", got)
	}
	usage := ai.Usage{Input: 10, Output: 5, CacheRead: 3, CacheWrite: 2}
	if got := CalculateContextTokens(usage); got != 20 {
		t.Errorf("got %d, want 20", got)
	}
}

func TestEstimateContextTokensAnchorsOnLastUsage(t *testing.T) {
	messages := []agent.AgentMessage{
		userMessage(strings.Repeat("a", 400)),
		assistantMessage("done", 5000),
		userMessage(strings.Repeat("b", 400)),
	}

	estimate := EstimateContextTokens(messages)
	if estimate.UsageTokens != 5000 {
		t.Errorf("usageTokens = %d, want 5000", estimate.UsageTokens)
	}
	if estimate.TrailingTokens != 100 {
		t.Errorf("trailingTokens = %d, want 100", estimate.TrailingTokens)
	}
	if estimate.Tokens != 5100 {
		t.Errorf("tokens = %d, want 5100", estimate.Tokens)
	}
	if estimate.LastUsageIndex != 1 {
		t.Errorf("lastUsageIndex = %d, want 1", estimate.LastUsageIndex)
	}
}

func TestEstimateContextTokensIgnoresFailedTurns(t *testing.T) {
	failed := assistantMessage("", 0)
	failed.StopReason = ai.StopError
	failed.Usage = &ai.Usage{TotalTokens: 99999}

	messages := []agent.AgentMessage{userMessage(strings.Repeat("a", 40)), failed}
	estimate := EstimateContextTokens(messages)
	if estimate.LastUsageIndex != -1 {
		t.Errorf("a failed turn should not anchor the estimate, got index %d", estimate.LastUsageIndex)
	}
	if estimate.Tokens != 10 {
		t.Errorf("tokens = %d, want 10", estimate.Tokens)
	}
}

func TestFindCutPointNeverCutsAtToolResult(t *testing.T) {
	messages := []agent.AgentMessage{
		userMessage(strings.Repeat("a", 4000)),
		{Role: "assistant", AssistantContent: []ai.AssistantContentBlock{
			ai.ToolCallBlock(ai.ToolCall{ID: "1", Name: "read", Arguments: map[string]any{"path": "/tmp/x"}}),
		}},
		{Role: "toolResult", ToolCallID: "1", ToolName: "read",
			ToolContent: []ai.ToolResultContent{{Type: "text", Text: strings.Repeat("b", 4000)}}},
		userMessage(strings.Repeat("c", 4000)),
	}

	cut := FindCutPoint(messages, 1000)
	if cut == 2 {
		t.Fatal("cut point landed on a tool result")
	}
	if messages[cut].Role == "toolResult" {
		t.Fatalf("cut point %d is a tool result", cut)
	}
}

func TestFindCutPointKeepsEverythingUnderBudget(t *testing.T) {
	messages := []agent.AgentMessage{userMessage("hi"), assistantMessage("hello", 0)}
	if cut := FindCutPoint(messages, 100_000); cut != 0 {
		t.Errorf("cut = %d, want 0 when the budget covers the transcript", cut)
	}
}

func TestEffectiveKeepRecentTokensShrinksForSmallWindows(t *testing.T) {
	settings := Settings{Enabled: true, ReserveTokens: 16384, KeepRecentTokens: 20000}

	if got := EffectiveKeepRecentTokens(200_000, settings); got != 20000 {
		t.Errorf("large window should keep the configured budget, got %d", got)
	}
	if got := EffectiveKeepRecentTokens(32_000, settings); got >= 20000 {
		t.Errorf("small window should shrink the budget, got %d", got)
	}
	if got := EffectiveKeepRecentTokens(8_000, settings); got != MinKeepRecentTokens {
		t.Errorf("tiny window should clamp to the floor, got %d", got)
	}
}

func TestCompactReplacesHistoryWithSummary(t *testing.T) {
	messages := []agent.AgentMessage{
		userMessage(strings.Repeat("old ", 2000)),
		{Role: "assistant", AssistantContent: []ai.AssistantContentBlock{
			ai.ToolCallBlock(ai.ToolCall{ID: "1", Name: "write", Arguments: map[string]any{"path": "/repo/main.go"}}),
		}},
		{Role: "toolResult", ToolCallID: "1", ToolName: "write",
			ToolContent: []ai.ToolResultContent{{Type: "text", Text: "ok"}}},
		userMessage(strings.Repeat("recent ", 200)),
		assistantMessage("recent reply", 0),
	}

	result, err := Compact(context.Background(), Options{
		Messages: messages,
		Model:    ai.Model{ID: "test", ContextWindow: 100_000, MaxTokens: 4096},
		Settings: Settings{Enabled: true, ReserveTokens: 1000, KeepRecentTokens: 300},
		StreamFn: stubStreamFn("SUMMARY TEXT"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.MessagesRemoved == 0 {
		t.Fatal("want some messages summarized away")
	}
	if len(result.Messages) != len(messages)-result.MessagesRemoved+1 {
		t.Errorf("unexpected message count %d", len(result.Messages))
	}
	if result.Messages[0].Role != "user" {
		t.Errorf("summary message role = %q", result.Messages[0].Role)
	}
	summaryText := UserText(result.Messages[0].UserContent)
	if !strings.HasPrefix(summaryText, SummaryPrefix) {
		t.Errorf("summary message should be tagged, got %q", summaryText)
	}
	if !strings.Contains(summaryText, "SUMMARY TEXT") {
		t.Error("summary text missing")
	}
	if !strings.Contains(summaryText, "/repo/main.go") {
		t.Error("modified files should be recorded in the summary")
	}
	if result.Messages[1].Role == "toolResult" {
		t.Error("compacted context must not start its history with a tool result")
	}
}

func TestCompactReturnsErrNothingToCompact(t *testing.T) {
	messages := []agent.AgentMessage{userMessage("hi"), assistantMessage("hello", 0)}
	_, err := Compact(context.Background(), Options{
		Messages: messages,
		Model:    ai.Model{ID: "test", ContextWindow: 100_000},
		Settings: DefaultSettings(),
		StreamFn: stubStreamFn("unused"),
	})
	if err != ErrNothingToCompact {
		t.Errorf("err = %v, want ErrNothingToCompact", err)
	}
}

func TestExtractPreviousSummary(t *testing.T) {
	messages := []agent.AgentMessage{
		userMessage("first"),
		SummaryMessage("## Goal\nShip the port"),
		userMessage("next"),
	}
	if got := ExtractPreviousSummary(messages); !strings.Contains(got, "Ship the port") {
		t.Errorf("got %q", got)
	}
	if got := ExtractPreviousSummary([]agent.AgentMessage{userMessage("no summary")}); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func stubStreamFn(text string) ai.StreamFn {
	return func(model ai.Model, _ ai.Context, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		stream := ai.NewAssistantMessageEventStream()
		message := ai.AssistantMessage{
			Role:       "assistant",
			Content:    []ai.AssistantContentBlock{ai.TextBlock(text)},
			Model:      model.ID,
			Usage:      ai.EmptyUsage(),
			StopReason: ai.StopStop,
		}
		stream.Push(ai.AssistantMessageEvent{Type: "done", Reason: ai.StopStop, Message: &message})
		return stream
	}
}

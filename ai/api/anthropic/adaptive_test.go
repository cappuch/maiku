package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/cappuch/maiku/ai"
)

func TestAdaptiveThinkingRequest(t *testing.T) {
	for _, id := range []string{"claude-opus-5-5", "claude-opus-5.5", "us.anthropic.claude-opus-5-5-v1:0", "claude-opus-4-6", "claude-sonnet-4-6"} {
		for level, effort := range map[ai.ThinkingLevel]string{
			ai.ThinkingMinimal: "low", ai.ThinkingLow: "low", ai.ThinkingMedium: "medium", ai.ThinkingHigh: "high",
		} {
			req, err := buildRequest(ai.Model{ID: id, Provider: "bedrock", Reasoning: true}, ai.Context{}, &ai.SimpleStreamOptions{Reasoning: level})
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(req)
			if err != nil {
				t.Fatal(err)
			}
			var wire map[string]any
			if err := json.Unmarshal(data, &wire); err != nil {
				t.Fatal(err)
			}
			thinking := wire["thinking"].(map[string]any)
			if thinking["type"] != "adaptive" || thinking["budget_tokens"] != nil || wire["output_config"].(map[string]any)["effort"] != effort {
				t.Fatalf("%s/%s: %s", id, level, data)
			}
		}
	}
}

func TestLegacyAndDisabledThinkingRequest(t *testing.T) {
	for _, tc := range []struct {
		id    string
		level ai.ThinkingLevel
		kind  string
	}{
		{"claude-opus-4-5-20251101", ai.ThinkingMedium, "enabled"},
		{"claude-sonnet-4-20250514", ai.ThinkingMedium, "enabled"},
		{"claude-opus-5-5", ai.ThinkingOff, "disabled"},
	} {
		req, err := buildRequest(ai.Model{ID: tc.id, Reasoning: true}, ai.Context{}, &ai.SimpleStreamOptions{Reasoning: tc.level})
		if err != nil {
			t.Fatal(err)
		}
		if req.Thinking.Type != tc.kind || req.OutputConfig != nil {
			t.Fatalf("%s: %+v", tc.id, req)
		}
		if tc.kind == "enabled" && req.Thinking.BudgetTokens < 1024 {
			t.Fatal("missing legacy thinking budget")
		}
	}
}

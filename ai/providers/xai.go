package providers

import (
	// Registers ai.APIOpenAICompletions with the ai package's stream registry.
	_ "github.com/mikus/maiku/ai/api/openaicompletions"

	"github.com/mikus/maiku/ai"
)

const xaiBaseURL = "https://api.x.ai/v1"

// XAI returns the built-in xAI (Grok) provider. xAI exposes an OpenAI Chat
// Completions compatible endpoint (it also has a Responses API, not ported
// here), so this uses ai/api/openaicompletions.
func XAI() Provider {
	models := []ai.Model{
		{
			ID:            "grok-4",
			Name:          "Grok 4 (xAI)",
			API:           ai.APIOpenAICompletions,
			Provider:      "xai",
			BaseURL:       xaiBaseURL,
			Reasoning:     true,
			Input:         []string{"text", "image"},
			ContextWindow: 256000,
			MaxTokens:     64000,
			Cost: ai.ModelCost{
				ModelCostRates: ai.ModelCostRates{Input: 3, Output: 15, CacheRead: 0.75, CacheWrite: 0},
			},
		},
		{
			ID:            "grok-4-fast",
			Name:          "Grok 4 Fast (xAI)",
			API:           ai.APIOpenAICompletions,
			Provider:      "xai",
			BaseURL:       xaiBaseURL,
			Reasoning:     true,
			Input:         []string{"text", "image"},
			ContextWindow: 2000000,
			MaxTokens:     30000,
			Cost: ai.ModelCost{
				ModelCostRates: ai.ModelCostRates{Input: 0.2, Output: 0.5, CacheRead: 0.05, CacheWrite: 0},
			},
		},
		{
			ID:            "grok-code-fast-1",
			Name:          "Grok Code Fast 1 (xAI)",
			API:           ai.APIOpenAICompletions,
			Provider:      "xai",
			BaseURL:       xaiBaseURL,
			Reasoning:     true,
			Input:         []string{"text"},
			ContextWindow: 256000,
			MaxTokens:     10000,
			Cost: ai.ModelCost{
				ModelCostRates: ai.ModelCostRates{Input: 0.2, Output: 1.5, CacheRead: 0.02, CacheWrite: 0},
			},
		},
	}

	return Provider{
		ID:      "xai",
		Name:    "xAI",
		BaseURL: xaiBaseURL,
		Models:  models,
	}
}

package providers

import (
	// Registers ai.APIOpenAICompletions with the ai package's stream registry.
	_ "github.com/mikus/maiku/ai/api/openaicompletions"

	"github.com/mikus/maiku/ai"
)

const fireworksBaseURL = "https://api.fireworks.ai/inference/v1"

// Fireworks returns the built-in Fireworks AI provider. Fireworks can serve
// some models over Anthropic's Messages API, but the models below use its
// OpenAI Chat Completions compatible endpoint (ai/api/openaicompletions).
func Fireworks() Provider {
	models := []ai.Model{
		{
			ID:            "accounts/fireworks/models/llama-v3p3-70b-instruct",
			Name:          "Llama 3.3 70B Instruct (Fireworks)",
			API:           ai.APIOpenAICompletions,
			Provider:      "fireworks",
			BaseURL:       fireworksBaseURL,
			Reasoning:     false,
			Input:         []string{"text"},
			ContextWindow: 131072,
			MaxTokens:     16384,
			Cost: ai.ModelCost{
				ModelCostRates: ai.ModelCostRates{Input: 0.9, Output: 0.9, CacheRead: 0, CacheWrite: 0},
			},
		},
		{
			ID:            "accounts/fireworks/models/deepseek-v3",
			Name:          "DeepSeek V3 (Fireworks)",
			API:           ai.APIOpenAICompletions,
			Provider:      "fireworks",
			BaseURL:       fireworksBaseURL,
			Reasoning:     false,
			Input:         []string{"text"},
			ContextWindow: 128000,
			MaxTokens:     8192,
			Cost: ai.ModelCost{
				ModelCostRates: ai.ModelCostRates{Input: 0.9, Output: 0.9, CacheRead: 0, CacheWrite: 0},
			},
		},
	}

	return Provider{
		ID:      "fireworks",
		Name:    "Fireworks",
		BaseURL: fireworksBaseURL,
		Models:  models,
	}
}

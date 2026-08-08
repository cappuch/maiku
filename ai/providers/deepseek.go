package providers

import (
	// Registers ai.APIOpenAICompletions with the ai package's stream registry.
	_ "github.com/mikus/maiku/ai/api/openaicompletions"

	"github.com/mikus/maiku/ai"
)

const deepseekBaseURL = "https://api.deepseek.com"

// DeepSeek returns the built-in DeepSeek provider. DeepSeek's API is
// OpenAI Chat Completions compatible, so it uses ai/api/openaicompletions.
func DeepSeek() Provider {
	models := []ai.Model{
		{
			ID:            "deepseek-chat",
			Name:          "DeepSeek Chat (V3)",
			API:           ai.APIOpenAICompletions,
			Provider:      "deepseek",
			BaseURL:       deepseekBaseURL,
			Reasoning:     false,
			Input:         []string{"text"},
			ContextWindow: 64000,
			MaxTokens:     8192,
			Cost: ai.ModelCost{
				ModelCostRates: ai.ModelCostRates{Input: 0.27, Output: 1.1, CacheRead: 0.07, CacheWrite: 0},
			},
		},
		{
			ID:            "deepseek-reasoner",
			Name:          "DeepSeek Reasoner (R1)",
			API:           ai.APIOpenAICompletions,
			Provider:      "deepseek",
			BaseURL:       deepseekBaseURL,
			Reasoning:     true,
			Input:         []string{"text"},
			ContextWindow: 64000,
			MaxTokens:     64000,
			Cost: ai.ModelCost{
				ModelCostRates: ai.ModelCostRates{Input: 0.55, Output: 2.19, CacheRead: 0.14, CacheWrite: 0},
			},
		},
	}

	return Provider{
		ID:      "deepseek",
		Name:    "DeepSeek",
		BaseURL: deepseekBaseURL,
		Models:  models,
	}
}

package providers

import (
	// Registers ai.APIOpenAICompletions with the ai package's stream registry.
	_ "github.com/mikus/maiku/ai/api/openaicompletions"

	"github.com/mikus/maiku/ai"
)

const togetherBaseURL = "https://api.together.ai/v1"

// Together returns the built-in Together AI provider. Together speaks the
// OpenAI Chat Completions wire format for the open models it hosts, so it
// uses ai/api/openaicompletions.
func Together() Provider {
	models := []ai.Model{
		{
			ID:            "meta-llama/Llama-3.3-70B-Instruct-Turbo",
			Name:          "Llama 3.3 70B Instruct Turbo (Together)",
			API:           ai.APIOpenAICompletions,
			Provider:      "together",
			BaseURL:       togetherBaseURL,
			Reasoning:     false,
			Input:         []string{"text"},
			ContextWindow: 131072,
			MaxTokens:     16384,
			Cost: ai.ModelCost{
				ModelCostRates: ai.ModelCostRates{Input: 0.88, Output: 0.88, CacheRead: 0, CacheWrite: 0},
			},
		},
		{
			ID:            "deepseek-ai/DeepSeek-V3",
			Name:          "DeepSeek V3 (Together)",
			API:           ai.APIOpenAICompletions,
			Provider:      "together",
			BaseURL:       togetherBaseURL,
			Reasoning:     false,
			Input:         []string{"text"},
			ContextWindow: 128000,
			MaxTokens:     8192,
			Cost: ai.ModelCost{
				ModelCostRates: ai.ModelCostRates{Input: 1.25, Output: 1.25, CacheRead: 0, CacheWrite: 0},
			},
		},
		{
			ID:            "Qwen/Qwen2.5-72B-Instruct-Turbo",
			Name:          "Qwen 2.5 72B Instruct Turbo (Together)",
			API:           ai.APIOpenAICompletions,
			Provider:      "together",
			BaseURL:       togetherBaseURL,
			Reasoning:     false,
			Input:         []string{"text"},
			ContextWindow: 32768,
			MaxTokens:     8192,
			Cost: ai.ModelCost{
				ModelCostRates: ai.ModelCostRates{Input: 1.2, Output: 1.2, CacheRead: 0, CacheWrite: 0},
			},
		},
	}

	return Provider{
		ID:      "together",
		Name:    "Together",
		BaseURL: togetherBaseURL,
		Models:  models,
	}
}

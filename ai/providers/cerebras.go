package providers

import (
	// Registers ai.APIOpenAICompletions with the ai package's stream registry.
	_ "github.com/mikus/maiku/ai/api/openaicompletions"

	"github.com/mikus/maiku/ai"
)

const cerebrasBaseURL = "https://api.cerebras.ai/v1"

// Cerebras returns the built-in Cerebras provider. Cerebras speaks the
// OpenAI Chat Completions wire format for the open models it serves on
// wafer-scale inference hardware, so it uses ai/api/openaicompletions.
func Cerebras() Provider {
	models := []ai.Model{
		{
			ID:            "llama-3.3-70b",
			Name:          "Llama 3.3 70B (Cerebras)",
			API:           ai.APIOpenAICompletions,
			Provider:      "cerebras",
			BaseURL:       cerebrasBaseURL,
			Reasoning:     false,
			Input:         []string{"text"},
			ContextWindow: 128000,
			MaxTokens:     8192,
			Cost: ai.ModelCost{
				ModelCostRates: ai.ModelCostRates{Input: 0.85, Output: 1.2, CacheRead: 0, CacheWrite: 0},
			},
		},
		{
			ID:            "qwen-3-32b",
			Name:          "Qwen 3 32B (Cerebras)",
			API:           ai.APIOpenAICompletions,
			Provider:      "cerebras",
			BaseURL:       cerebrasBaseURL,
			Reasoning:     true,
			Input:         []string{"text"},
			ContextWindow: 131072,
			MaxTokens:     16384,
			Cost: ai.ModelCost{
				ModelCostRates: ai.ModelCostRates{Input: 0.4, Output: 0.8, CacheRead: 0, CacheWrite: 0},
			},
		},
		{
			ID:            "gpt-oss-120b",
			Name:          "GPT-OSS 120B (Cerebras)",
			API:           ai.APIOpenAICompletions,
			Provider:      "cerebras",
			BaseURL:       cerebrasBaseURL,
			Reasoning:     true,
			Input:         []string{"text"},
			ContextWindow: 131072,
			MaxTokens:     8192,
			Cost: ai.ModelCost{
				ModelCostRates: ai.ModelCostRates{Input: 0.35, Output: 0.75, CacheRead: 0, CacheWrite: 0},
			},
		},
	}

	return Provider{
		ID:      "cerebras",
		Name:    "Cerebras",
		BaseURL: cerebrasBaseURL,
		Models:  models,
	}
}

package providers

import (
	// Registers ai.APIOpenAICompletions with the ai package's stream registry.
	_ "github.com/mikus/maiku/ai/api/openaicompletions"

	"github.com/mikus/maiku/ai"
)

const openrouterBaseURL = "https://openrouter.ai/api/v1"

// OpenRouter returns the built-in OpenRouter provider. OpenRouter speaks the
// OpenAI Chat Completions wire format for every model it proxies, so it uses
// ai/api/openaicompletions regardless of the upstream model's native API.
func OpenRouter() Provider {
	models := []ai.Model{
		{
			ID:            "anthropic/claude-sonnet-4.5",
			Name:          "Claude Sonnet 4.5 (OpenRouter)",
			API:           ai.APIOpenAICompletions,
			Provider:      "openrouter",
			BaseURL:       openrouterBaseURL,
			Reasoning:     true,
			Input:         []string{"text", "image"},
			ContextWindow: 200000,
			MaxTokens:     64000,
			Cost: ai.ModelCost{
				ModelCostRates: ai.ModelCostRates{Input: 3, Output: 15, CacheRead: 0.3, CacheWrite: 3.75},
			},
		},
		{
			ID:            "openai/gpt-5",
			Name:          "GPT-5 (OpenRouter)",
			API:           ai.APIOpenAICompletions,
			Provider:      "openrouter",
			BaseURL:       openrouterBaseURL,
			Reasoning:     true,
			Input:         []string{"text", "image"},
			ContextWindow: 400000,
			MaxTokens:     128000,
			Cost: ai.ModelCost{
				ModelCostRates: ai.ModelCostRates{Input: 1.25, Output: 10, CacheRead: 0.125, CacheWrite: 0},
			},
		},
		{
			ID:            "google/gemini-2.5-pro",
			Name:          "Gemini 2.5 Pro (OpenRouter)",
			API:           ai.APIOpenAICompletions,
			Provider:      "openrouter",
			BaseURL:       openrouterBaseURL,
			Reasoning:     true,
			Input:         []string{"text", "image"},
			ContextWindow: 1048576,
			MaxTokens:     65536,
			Cost: ai.ModelCost{
				ModelCostRates: ai.ModelCostRates{Input: 1.25, Output: 10, CacheRead: 0.31, CacheWrite: 0},
			},
		},
		{
			ID:            "meta-llama/llama-3.3-70b-instruct",
			Name:          "Llama 3.3 70B Instruct (OpenRouter)",
			API:           ai.APIOpenAICompletions,
			Provider:      "openrouter",
			BaseURL:       openrouterBaseURL,
			Reasoning:     false,
			Input:         []string{"text"},
			ContextWindow: 131072,
			MaxTokens:     16384,
			Cost: ai.ModelCost{
				ModelCostRates: ai.ModelCostRates{Input: 0.12, Output: 0.3, CacheRead: 0, CacheWrite: 0},
			},
		},
	}

	return Provider{
		ID:      "openrouter",
		Name:    "OpenRouter",
		BaseURL: openrouterBaseURL,
		Models:  models,
	}
}

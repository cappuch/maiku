package providers

import (
	// Registers ai.APIOpenAICompletions with the ai package's stream registry.
	_ "github.com/mikus/maiku/ai/api/openaicompletions"
	// Registers ai.APIOpenAIResponses with the ai package's stream registry.
	_ "github.com/mikus/maiku/ai/api/openairesponses"

	"github.com/mikus/maiku/ai"
)

const openaiBaseURL = "https://api.openai.com/v1"

// OpenAI returns the built-in OpenAI provider. gpt-5* models use the newer
// Responses API (ai/api/openairesponses), matching pi-ai; older non-reasoning
// models stay on the OpenAI-compatible /chat/completions endpoint
// (ai/api/openaicompletions).
func OpenAI() Provider {
	models := []ai.Model{
		{
			ID:            "gpt-5",
			Name:          "GPT-5",
			API:           ai.APIOpenAIResponses,
			Provider:      "openai",
			BaseURL:       openaiBaseURL,
			Reasoning:     true,
			Input:         []string{"text", "image"},
			ContextWindow: 400000,
			MaxTokens:     128000,
			Cost: ai.ModelCost{
				ModelCostRates: ai.ModelCostRates{Input: 1.25, Output: 10, CacheRead: 0.125, CacheWrite: 0},
			},
		},
		{
			ID:            "gpt-5-mini",
			Name:          "GPT-5 Mini",
			API:           ai.APIOpenAIResponses,
			Provider:      "openai",
			BaseURL:       openaiBaseURL,
			Reasoning:     true,
			Input:         []string{"text", "image"},
			ContextWindow: 400000,
			MaxTokens:     128000,
			Cost: ai.ModelCost{
				ModelCostRates: ai.ModelCostRates{Input: 0.25, Output: 2, CacheRead: 0.025, CacheWrite: 0},
			},
		},
		{
			ID:            "gpt-4o-mini",
			Name:          "GPT-4o Mini",
			API:           ai.APIOpenAICompletions,
			Provider:      "openai",
			BaseURL:       openaiBaseURL,
			Reasoning:     false,
			Input:         []string{"text", "image"},
			ContextWindow: 128000,
			MaxTokens:     16384,
			Cost: ai.ModelCost{
				ModelCostRates: ai.ModelCostRates{Input: 0.15, Output: 0.6, CacheRead: 0.075, CacheWrite: 0},
			},
		},
	}

	return Provider{
		ID:      "openai",
		Name:    "OpenAI",
		BaseURL: openaiBaseURL,
		Models:  models,
	}
}

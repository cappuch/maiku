package providers

import (
	// Registers ai.APIOpenAICompletions with the ai package's stream registry.
	_ "github.com/mikus/maiku/ai/api/openaicompletions"

	"github.com/mikus/maiku/ai"
)

const groqBaseURL = "https://api.groq.com/openai/v1"

// Groq returns the built-in Groq provider. Groq speaks the OpenAI Chat
// Completions wire format for the open models it serves on custom
// inference hardware, so it uses ai/api/openaicompletions.
func Groq() Provider {
	models := []ai.Model{
		{
			ID:            "llama-3.3-70b-versatile",
			Name:          "Llama 3.3 70B Versatile (Groq)",
			API:           ai.APIOpenAICompletions,
			Provider:      "groq",
			BaseURL:       groqBaseURL,
			Reasoning:     false,
			Input:         []string{"text"},
			ContextWindow: 128000,
			MaxTokens:     32768,
			Cost: ai.ModelCost{
				ModelCostRates: ai.ModelCostRates{Input: 0.59, Output: 0.79, CacheRead: 0, CacheWrite: 0},
			},
		},
		{
			ID:            "openai/gpt-oss-120b",
			Name:          "GPT-OSS 120B (Groq)",
			API:           ai.APIOpenAICompletions,
			Provider:      "groq",
			BaseURL:       groqBaseURL,
			Reasoning:     true,
			Input:         []string{"text"},
			ContextWindow: 131072,
			MaxTokens:     32768,
			Cost: ai.ModelCost{
				ModelCostRates: ai.ModelCostRates{Input: 0.15, Output: 0.75, CacheRead: 0, CacheWrite: 0},
			},
		},
		{
			ID:            "moonshotai/kimi-k2-instruct",
			Name:          "Kimi K2 Instruct (Groq)",
			API:           ai.APIOpenAICompletions,
			Provider:      "groq",
			BaseURL:       groqBaseURL,
			Reasoning:     false,
			Input:         []string{"text"},
			ContextWindow: 131072,
			MaxTokens:     16384,
			Cost: ai.ModelCost{
				ModelCostRates: ai.ModelCostRates{Input: 1, Output: 3, CacheRead: 0, CacheWrite: 0},
			},
		},
	}

	return Provider{
		ID:      "groq",
		Name:    "Groq",
		BaseURL: groqBaseURL,
		Models:  models,
	}
}

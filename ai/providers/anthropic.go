// Package providers exposes built-in provider definitions (id, base URL,
// static model catalog) for the APIs implemented under ai/api/*. Importing
// this package (or the individual ai/api/* packages) registers their
// streaming implementations with ai.StreamSimple via init().
package providers

import (
	// Registers ai.APIAnthropicMessages with the ai package's stream registry.
	_ "github.com/mikus/maiku/ai/api/anthropic"

	"github.com/mikus/maiku/ai"
)

// Provider is a static description of a model provider: where its API lives
// and which models it exposes.
type Provider struct {
	ID      string
	Name    string
	BaseURL string
	Models  []ai.Model
}

const anthropicBaseURL = "https://api.anthropic.com"

// Anthropic returns the built-in Anthropic provider with a small, current
// set of Claude models over the Anthropic Messages API.
func Anthropic() Provider {
	models := []ai.Model{
		{
			ID:            "claude-opus-4-6",
			Name:          "Claude Opus 4.6",
			API:           ai.APIAnthropicMessages,
			Provider:      "anthropic",
			BaseURL:       anthropicBaseURL,
			Reasoning:     true,
			Input:         []string{"text", "image"},
			ContextWindow: 200000,
			MaxTokens:     32000,
			Cost: ai.ModelCost{
				ModelCostRates: ai.ModelCostRates{Input: 15, Output: 75, CacheRead: 1.5, CacheWrite: 18.75},
			},
		},
		{
			ID:            "claude-sonnet-4-6",
			Name:          "Claude Sonnet 4.6",
			API:           ai.APIAnthropicMessages,
			Provider:      "anthropic",
			BaseURL:       anthropicBaseURL,
			Reasoning:     true,
			Input:         []string{"text", "image"},
			ContextWindow: 200000,
			MaxTokens:     64000,
			Cost: ai.ModelCost{
				ModelCostRates: ai.ModelCostRates{Input: 3, Output: 15, CacheRead: 0.3, CacheWrite: 3.75},
			},
		},
		{
			ID:            "claude-sonnet-4-5",
			Name:          "Claude Sonnet 4.5",
			API:           ai.APIAnthropicMessages,
			Provider:      "anthropic",
			BaseURL:       anthropicBaseURL,
			Reasoning:     true,
			Input:         []string{"text", "image"},
			ContextWindow: 200000,
			MaxTokens:     64000,
			Cost: ai.ModelCost{
				ModelCostRates: ai.ModelCostRates{Input: 3, Output: 15, CacheRead: 0.3, CacheWrite: 3.75},
			},
		},
		{
			ID:            "claude-haiku-4-5",
			Name:          "Claude Haiku 4.5",
			API:           ai.APIAnthropicMessages,
			Provider:      "anthropic",
			BaseURL:       anthropicBaseURL,
			Reasoning:     false,
			Input:         []string{"text", "image"},
			ContextWindow: 200000,
			MaxTokens:     8192,
			Cost: ai.ModelCost{
				ModelCostRates: ai.ModelCostRates{Input: 1, Output: 5, CacheRead: 0.1, CacheWrite: 1.25},
			},
		},
	}

	return Provider{
		ID:      "anthropic",
		Name:    "Anthropic",
		BaseURL: anthropicBaseURL,
		Models:  models,
	}
}

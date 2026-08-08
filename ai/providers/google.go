package providers

import (
	// Registers ai.APIGoogleGenerativeAI with the ai package's stream registry.
	_ "github.com/mikus/maiku/ai/api/google"

	"github.com/mikus/maiku/ai"
)

const googleBaseURL = "https://generativelanguage.googleapis.com/v1beta"

// Google returns the built-in Google Generative AI (Gemini) provider.
func Google() Provider {
	models := []ai.Model{
		{
			ID:            "gemini-2.5-pro",
			Name:          "Gemini 2.5 Pro",
			API:           ai.APIGoogleGenerativeAI,
			Provider:      "google",
			BaseURL:       googleBaseURL,
			Reasoning:     true,
			Input:         []string{"text", "image"},
			ContextWindow: 1048576,
			MaxTokens:     65536,
			Cost: ai.ModelCost{
				ModelCostRates: ai.ModelCostRates{Input: 1.25, Output: 10, CacheRead: 0.315, CacheWrite: 0},
			},
		},
		{
			ID:            "gemini-2.5-flash",
			Name:          "Gemini 2.5 Flash",
			API:           ai.APIGoogleGenerativeAI,
			Provider:      "google",
			BaseURL:       googleBaseURL,
			Reasoning:     false,
			Input:         []string{"text", "image"},
			ContextWindow: 1048576,
			MaxTokens:     65536,
			Cost: ai.ModelCost{
				ModelCostRates: ai.ModelCostRates{Input: 0.3, Output: 2.5, CacheRead: 0.075, CacheWrite: 0},
			},
		},
		{
			ID:            "gemini-2.0-flash",
			Name:          "Gemini 2.0 Flash",
			API:           ai.APIGoogleGenerativeAI,
			Provider:      "google",
			BaseURL:       googleBaseURL,
			Reasoning:     false,
			Input:         []string{"text", "image"},
			ContextWindow: 1048576,
			MaxTokens:     8192,
			Cost: ai.ModelCost{
				ModelCostRates: ai.ModelCostRates{Input: 0.1, Output: 0.4, CacheRead: 0.025, CacheWrite: 0},
			},
		},
	}

	return Provider{
		ID:      "google",
		Name:    "Google",
		BaseURL: googleBaseURL,
		Models:  models,
	}
}

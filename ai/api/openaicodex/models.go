package openaicodex

import "github.com/mikus/maiku/ai"

const providerID = "openai-codex"

// StaticModels returns the ChatGPT Codex subscription model catalog.
// Codex has no public /models route, so these are baked in (from pi-ai).
func StaticModels() []ai.Model {
	base := defaultBaseURL
	mk := func(id, name string, input []string, cost ai.ModelCost, ctxWindow, maxTokens int) ai.Model {
		if len(input) == 0 {
			input = []string{"text", "image"}
		}
		if ctxWindow == 0 {
			ctxWindow = 272000
		}
		if maxTokens == 0 {
			maxTokens = 128000
		}
		return ai.Model{
			ID:            id,
			Name:          name,
			API:           ai.APIOpenAICodexResponses,
			Provider:      providerID,
			BaseURL:       base,
			Reasoning:     true,
			Input:         input,
			Cost:          cost,
			ContextWindow: ctxWindow,
			MaxTokens:     maxTokens,
		}
	}
	c := func(in, out, cacheRead float64) ai.ModelCost {
		return ai.ModelCost{ModelCostRates: ai.ModelCostRates{
			Input: in, Output: out, CacheRead: cacheRead, CacheWrite: 0,
		}}
	}
	return []ai.Model{
		mk("gpt-5.3-codex-spark", "GPT-5.3 Codex Spark", []string{"text"}, c(0, 0, 0), 128000, 128000),

		mk("gpt-5.4", "GPT-5.4", nil, c(2.5, 15, 0.25), 1050000, 128000),
		mk("gpt-5.4-mini", "GPT-5.4 Mini", nil, c(0.75, 4.5, 0.075), 400000, 128000),

		mk("gpt-5.5", "GPT-5.5", nil, c(5, 30, 0.5), 1050000, 128000),

		mk("gpt-5.6-sol", "GPT-5.6 Sol", nil, c(5, 30, 0.5), 1050000, 128000),
		mk("gpt-5.6-terra", "GPT-5.6 Terra", nil, c(2, 12, 0.2), 1050000, 128000),
		mk("gpt-5.6-luna", "GPT-5.6 Luna", nil, c(0.2, 1.2, 0.02), 1050000, 128000),
	}
}

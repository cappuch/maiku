package providers

import "github.com/mikus/maiku/ai"

// All returns every built-in provider in a stable order.
func All() []Provider {
	return []Provider{
		Anthropic(),
		OpenAI(),
		Google(),
		OpenRouter(),
		Mistral(),
		Groq(),
		DeepSeek(),
		Together(),
		Fireworks(),
		Cerebras(),
		XAI(),
		HuggingFace(),
		Nvidia(),
		Baseten(),
		MoonshotAI(),
		MoonshotAICN(),
		ZAI(),
		ZAICN(),
		AntLing(),
		KimiCoding(),
		MiniMax(),
		MiniMaxCN(),
		VercelAIGateway(),
		OpenCode(),
		OpenCodeGo(),
		Xiaomi(),
	}
}

// AllModels returns every model across every built-in provider.
func AllModels() []ai.Model {
	var out []ai.Model
	for _, p := range All() {
		out = append(out, p.Models...)
	}
	return out
}

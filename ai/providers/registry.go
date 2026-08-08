// Package providers exposes built-in provider connection metadata (id, name,
// base URL, default API). Model catalogs are fetched at runtime from each
// provider's /models route — nothing is stored statically here.
package providers

import (
	_ "github.com/mikus/maiku/ai/api/anthropic"
	_ "github.com/mikus/maiku/ai/api/google"
	_ "github.com/mikus/maiku/ai/api/openaicompletions"
	_ "github.com/mikus/maiku/ai/api/openaicodex"
	_ "github.com/mikus/maiku/ai/api/openairesponses"

	"github.com/mikus/maiku/ai"
)

// Provider describes where a model API lives. Models are loaded dynamically.
type Provider struct {
	ID      string
	Name    string
	BaseURL string
	// API is the default streaming API used for models discovered from this
	// provider's models route.
	API string
	// Models is filled at runtime after a successful /models fetch. It is
	// never populated statically in this package.
	Models []ai.Model
}

// All returns every known provider in a stable order (no static model lists).
func All() []Provider {
	return []Provider{
		{ID: "anthropic", Name: "Anthropic", BaseURL: "https://api.anthropic.com", API: ai.APIAnthropicMessages},
		{ID: "openai", Name: "OpenAI", BaseURL: "https://api.openai.com/v1", API: ai.APIOpenAIResponses},
		{ID: "openai-codex", Name: "OpenAI Codex", BaseURL: "https://chatgpt.com/backend-api", API: ai.APIOpenAICodexResponses},
		{ID: "google", Name: "Google", BaseURL: "https://generativelanguage.googleapis.com/v1beta", API: ai.APIGoogleGenerativeAI},
		{ID: "openrouter", Name: "OpenRouter", BaseURL: "https://openrouter.ai/api/v1", API: ai.APIOpenAICompletions},
		{ID: "mistral", Name: "Mistral", BaseURL: "https://api.mistral.ai/v1", API: ai.APIOpenAICompletions},
		{ID: "groq", Name: "Groq", BaseURL: "https://api.groq.com/openai/v1", API: ai.APIOpenAICompletions},
		{ID: "deepseek", Name: "DeepSeek", BaseURL: "https://api.deepseek.com", API: ai.APIOpenAICompletions},
		{ID: "together", Name: "Together", BaseURL: "https://api.together.ai/v1", API: ai.APIOpenAICompletions},
		{ID: "fireworks", Name: "Fireworks", BaseURL: "https://api.fireworks.ai/inference/v1", API: ai.APIOpenAICompletions},
		{ID: "cerebras", Name: "Cerebras", BaseURL: "https://api.cerebras.ai/v1", API: ai.APIOpenAICompletions},
		{ID: "xai", Name: "xAI", BaseURL: "https://api.x.ai/v1", API: ai.APIOpenAICompletions},
		{ID: "huggingface", Name: "Hugging Face", BaseURL: "https://router.huggingface.co/v1", API: ai.APIOpenAICompletions},
		{ID: "nvidia", Name: "NVIDIA NIM", BaseURL: "https://integrate.api.nvidia.com/v1", API: ai.APIOpenAICompletions},
		{ID: "baseten", Name: "Baseten", BaseURL: "https://inference.baseten.co/v1", API: ai.APIOpenAICompletions},
		{ID: "moonshotai", Name: "Moonshot AI", BaseURL: "https://api.moonshot.ai/v1", API: ai.APIOpenAICompletions},
		{ID: "moonshotai-cn", Name: "Moonshot AI (China)", BaseURL: "https://api.moonshot.cn/v1", API: ai.APIOpenAICompletions},
		{ID: "zai", Name: "Z.AI", BaseURL: "https://api.z.ai/api/coding/paas/v4", API: ai.APIOpenAICompletions},
		{ID: "zai-coding-cn", Name: "Z.AI Coding (China)", BaseURL: "https://open.bigmodel.cn/api/coding/paas/v4", API: ai.APIOpenAICompletions},
		{ID: "ant-ling", Name: "Ant Ling", BaseURL: "https://api.ant-ling.com/v1", API: ai.APIOpenAICompletions},
		{ID: "kimi-coding", Name: "Kimi For Coding", BaseURL: "https://api.kimi.com/coding", API: ai.APIAnthropicMessages},
		{ID: "minimax", Name: "MiniMax", BaseURL: "https://api.minimax.io/anthropic", API: ai.APIAnthropicMessages},
		{ID: "minimax-cn", Name: "MiniMax (China)", BaseURL: "https://api.minimaxi.com/anthropic", API: ai.APIAnthropicMessages},
		{ID: "vercel-ai-gateway", Name: "Vercel AI Gateway", BaseURL: "https://ai-gateway.vercel.sh", API: ai.APIAnthropicMessages},
		{ID: "opencode", Name: "OpenCode Zen", BaseURL: "https://opencode.ai/zen/v1", API: ai.APIOpenAICompletions},
		{ID: "opencode-go", Name: "OpenCode Go", BaseURL: "https://opencode.ai/zen/go/v1", API: ai.APIOpenAICompletions},
		{ID: "xiaomi", Name: "Xiaomi MiMo", BaseURL: "https://api.xiaomimimo.com/v1", API: ai.APIOpenAICompletions},
	}
}

// Find returns the provider with the given id.
func Find(id string) (Provider, bool) {
	for _, p := range All() {
		if p.ID == id {
			return p, true
		}
	}
	return Provider{}, false
}

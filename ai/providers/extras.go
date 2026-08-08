package providers

import (
	_ "github.com/mikus/maiku/ai/api/openaicompletions"

	"github.com/mikus/maiku/ai"
)

func modelOC(provider, baseURL, id, name string, reasoning bool, ctx, maxTok int, cost ai.ModelCostRates) ai.Model {
	return ai.Model{
		ID: id, Name: name, API: ai.APIOpenAICompletions, Provider: provider, BaseURL: baseURL,
		Reasoning: reasoning, Input: []string{"text"}, ContextWindow: ctx, MaxTokens: maxTok,
		Cost: ai.ModelCost{ModelCostRates: cost},
	}
}

func modelOCVision(provider, baseURL, id, name string, reasoning bool, ctx, maxTok int, cost ai.ModelCostRates) ai.Model {
	m := modelOC(provider, baseURL, id, name, reasoning, ctx, maxTok, cost)
	m.Input = []string{"text", "image"}
	return m
}

func modelAnthropicCompat(provider, baseURL, id, name string, reasoning bool, ctx, maxTok int, cost ai.ModelCostRates) ai.Model {
	return ai.Model{
		ID: id, Name: name, API: ai.APIAnthropicMessages, Provider: provider, BaseURL: baseURL,
		Reasoning: reasoning, Input: []string{"text", "image"}, ContextWindow: ctx, MaxTokens: maxTok,
		Cost: ai.ModelCost{ModelCostRates: cost},
	}
}

// HuggingFace — OpenAI-compatible router.
func HuggingFace() Provider {
	base := "https://router.huggingface.co/v1"
	return Provider{ID: "huggingface", Name: "Hugging Face", BaseURL: base, Models: []ai.Model{
		modelOC("huggingface", base, "meta-llama/Llama-3.3-70B-Instruct", "Llama 3.3 70B Instruct", false, 128000, 8192, ai.ModelCostRates{}),
		modelOC("huggingface", base, "Qwen/Qwen2.5-72B-Instruct", "Qwen2.5 72B Instruct", false, 128000, 8192, ai.ModelCostRates{}),
		modelOC("huggingface", base, "deepseek-ai/DeepSeek-V3", "DeepSeek V3", false, 64000, 8192, ai.ModelCostRates{}),
	}}
}

// Nvidia — NIM OpenAI-compatible API.
func Nvidia() Provider {
	base := "https://integrate.api.nvidia.com/v1"
	return Provider{ID: "nvidia", Name: "NVIDIA NIM", BaseURL: base, Models: []ai.Model{
		modelOC("nvidia", base, "meta/llama-3.3-70b-instruct", "Llama 3.3 70B Instruct", false, 128000, 8192, ai.ModelCostRates{}),
		modelOC("nvidia", base, "deepseek-ai/deepseek-r1", "DeepSeek R1", true, 128000, 8192, ai.ModelCostRates{}),
		modelOC("nvidia", base, "qwen/qwen2.5-coder-32b-instruct", "Qwen2.5 Coder 32B", false, 128000, 8192, ai.ModelCostRates{}),
	}}
}

// Baseten — OpenAI-compatible inference.
func Baseten() Provider {
	base := "https://inference.baseten.co/v1"
	return Provider{ID: "baseten", Name: "Baseten", BaseURL: base, Models: []ai.Model{
		modelOC("baseten", base, "openai/gpt-oss-120b", "GPT-OSS 120B", false, 128000, 8192, ai.ModelCostRates{}),
		modelOC("baseten", base, "moonshotai/Kimi-K2-Instruct", "Kimi K2 Instruct", false, 128000, 8192, ai.ModelCostRates{}),
	}}
}

// MoonshotAI — Kimi OpenAI-compatible API.
func MoonshotAI() Provider {
	base := "https://api.moonshot.ai/v1"
	return Provider{ID: "moonshotai", Name: "Moonshot AI", BaseURL: base, Models: []ai.Model{
		modelOCVision("moonshotai", base, "kimi-k2.5", "Kimi K2.5", true, 256000, 8192, ai.ModelCostRates{Input: 0.6, Output: 2.5}),
		modelOC("moonshotai", base, "kimi-k2-turbo-preview", "Kimi K2 Turbo", false, 256000, 8192, ai.ModelCostRates{Input: 0.15, Output: 2.5}),
		modelOC("moonshotai", base, "moonshot-v1-128k", "Moonshot v1 128K", false, 128000, 8192, ai.ModelCostRates{}),
	}}
}

// MoonshotAICN is the China endpoint for Moonshot.
func MoonshotAICN() Provider {
	base := "https://api.moonshot.cn/v1"
	p := MoonshotAI()
	p.ID = "moonshotai-cn"
	p.Name = "Moonshot AI (China)"
	p.BaseURL = base
	for i := range p.Models {
		p.Models[i].Provider = p.ID
		p.Models[i].BaseURL = base
	}
	return p
}

// ZAI — Zhipu / Z.AI coding plan (OpenAI-compatible).
func ZAI() Provider {
	base := "https://api.z.ai/api/coding/paas/v4"
	return Provider{ID: "zai", Name: "Z.AI", BaseURL: base, Models: []ai.Model{
		modelOC("zai", base, "glm-5", "GLM-5", true, 128000, 8192, ai.ModelCostRates{}),
		modelOC("zai", base, "glm-4.7", "GLM-4.7", true, 128000, 8192, ai.ModelCostRates{}),
		modelOC("zai", base, "glm-4.6", "GLM-4.6", false, 128000, 8192, ai.ModelCostRates{}),
	}}
}

// ZAICN is the China coding-plan endpoint.
func ZAICN() Provider {
	base := "https://open.bigmodel.cn/api/coding/paas/v4"
	p := ZAI()
	p.ID = "zai-coding-cn"
	p.Name = "Z.AI Coding (China)"
	p.BaseURL = base
	for i := range p.Models {
		p.Models[i].Provider = p.ID
		p.Models[i].BaseURL = base
	}
	return p
}

// AntLing — OpenAI-compatible.
func AntLing() Provider {
	base := "https://api.ant-ling.com/v1"
	return Provider{ID: "ant-ling", Name: "Ant Ling", BaseURL: base, Models: []ai.Model{
		modelOC("ant-ling", base, "ling-1t", "Ling 1T", true, 128000, 8192, ai.ModelCostRates{}),
		modelOC("ant-ling", base, "ling-flash-2.0", "Ling Flash 2.0", false, 128000, 8192, ai.ModelCostRates{}),
	}}
}

// Mistral uses the OpenAI-compatible chat completions endpoint.
func Mistral() Provider {
	base := "https://api.mistral.ai/v1"
	return Provider{ID: "mistral", Name: "Mistral", BaseURL: base, Models: []ai.Model{
		modelOCVision("mistral", base, "mistral-large-latest", "Mistral Large", false, 128000, 8192, ai.ModelCostRates{Input: 2, Output: 6}),
		modelOC("mistral", base, "mistral-medium-latest", "Mistral Medium", false, 128000, 8192, ai.ModelCostRates{Input: 0.4, Output: 2}),
		modelOC("mistral", base, "codestral-latest", "Codestral", false, 256000, 8192, ai.ModelCostRates{Input: 0.3, Output: 0.9}),
		modelOC("mistral", base, "devstral-medium-latest", "Devstral Medium", false, 128000, 8192, ai.ModelCostRates{}),
	}}
}

// KimiCoding — Anthropic-compatible Messages API.
func KimiCoding() Provider {
	base := "https://api.kimi.com/coding"
	return Provider{ID: "kimi-coding", Name: "Kimi For Coding", BaseURL: base, Models: []ai.Model{
		modelAnthropicCompat("kimi-coding", base, "kimi-k2.5", "Kimi K2.5", true, 256000, 8192, ai.ModelCostRates{}),
		modelAnthropicCompat("kimi-coding", base, "k2p5", "Kimi K2.5 (alias)", true, 256000, 8192, ai.ModelCostRates{}),
	}}
}

// MiniMax — Anthropic-compatible.
func MiniMax() Provider {
	base := "https://api.minimax.io/anthropic"
	return Provider{ID: "minimax", Name: "MiniMax", BaseURL: base, Models: []ai.Model{
		modelAnthropicCompat("minimax", base, "MiniMax-M2.5", "MiniMax M2.5", true, 200000, 8192, ai.ModelCostRates{}),
		modelAnthropicCompat("minimax", base, "MiniMax-M2.1", "MiniMax M2.1", true, 200000, 8192, ai.ModelCostRates{}),
	}}
}

// MiniMaxCN — China Anthropic-compatible endpoint.
func MiniMaxCN() Provider {
	base := "https://api.minimaxi.com/anthropic"
	p := MiniMax()
	p.ID = "minimax-cn"
	p.Name = "MiniMax (China)"
	p.BaseURL = base
	for i := range p.Models {
		p.Models[i].Provider = p.ID
		p.Models[i].BaseURL = base
	}
	return p
}

// VercelAIGateway — Anthropic Messages via Vercel AI Gateway.
func VercelAIGateway() Provider {
	base := "https://ai-gateway.vercel.sh"
	return Provider{ID: "vercel-ai-gateway", Name: "Vercel AI Gateway", BaseURL: base, Models: []ai.Model{
		modelAnthropicCompat("vercel-ai-gateway", base, "anthropic/claude-sonnet-4.5", "Claude Sonnet 4.5", true, 200000, 64000, ai.ModelCostRates{Input: 3, Output: 15}),
		modelAnthropicCompat("vercel-ai-gateway", base, "anthropic/claude-opus-4.6", "Claude Opus 4.6", true, 200000, 32000, ai.ModelCostRates{Input: 15, Output: 75}),
		modelAnthropicCompat("vercel-ai-gateway", base, "openai/gpt-5", "GPT-5", true, 200000, 8192, ai.ModelCostRates{}),
	}}
}

// OpenCode — OpenAI-compatible Zen API.
func OpenCode() Provider {
	base := "https://opencode.ai/zen/v1"
	return Provider{ID: "opencode", Name: "OpenCode Zen", BaseURL: base, Models: []ai.Model{
		modelOC("opencode", base, "claude-sonnet-4-5", "Claude Sonnet 4.5", true, 200000, 64000, ai.ModelCostRates{}),
		modelOC("opencode", base, "gpt-5", "GPT-5", true, 200000, 8192, ai.ModelCostRates{}),
	}}
}

// OpenCodeGo — OpenCode Go tier.
func OpenCodeGo() Provider {
	base := "https://opencode.ai/zen/go/v1"
	return Provider{ID: "opencode-go", Name: "OpenCode Go", BaseURL: base, Models: []ai.Model{
		modelOC("opencode-go", base, "glm-4.7", "GLM-4.7", true, 128000, 8192, ai.ModelCostRates{}),
		modelOC("opencode-go", base, "kimi-k2.5", "Kimi K2.5", true, 256000, 8192, ai.ModelCostRates{}),
	}}
}

// Xiaomi — MiMo OpenAI-compatible.
func Xiaomi() Provider {
	base := "https://api.xiaomimimo.com/v1"
	return Provider{ID: "xiaomi", Name: "Xiaomi MiMo", BaseURL: base, Models: []ai.Model{
		modelOC("xiaomi", base, "mimo-v2-flash", "MiMo V2 Flash", false, 128000, 8192, ai.ModelCostRates{}),
		modelOC("xiaomi", base, "mimo-v2-pro", "MiMo V2 Pro", true, 128000, 8192, ai.ModelCostRates{}),
	}}
}

// Together, Fireworks, etc. already exist as separate files.

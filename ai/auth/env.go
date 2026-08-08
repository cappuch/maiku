package auth

import (
	"os"
	"sync"
)

// providerEnvVars lists, in priority order, the environment variables that
// can supply an API key for a given provider id.
var providerEnvVars = map[string][]string{
	"anthropic":           {"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_OAUTH_TOKEN"},
	"openai":              {"OPENAI_API_KEY"},
	"openrouter":          {"OPENROUTER_API_KEY"},
	"groq":                {"GROQ_API_KEY"},
	"deepseek":            {"DEEPSEEK_API_KEY"},
	"together":            {"TOGETHER_API_KEY"},
	"fireworks":           {"FIREWORKS_API_KEY"},
	"cerebras":            {"CEREBRAS_API_KEY"},
	"xai":                 {"XAI_API_KEY"},
	"huggingface":         {"HF_TOKEN", "HUGGINGFACE_HUB_TOKEN"},
	"nvidia":              {"NVIDIA_API_KEY"},
	"baseten":             {"BASETEN_API_KEY"},
	"moonshotai":          {"MOONSHOT_API_KEY"},
	"moonshotai-cn":       {"MOONSHOT_API_KEY", "MOONSHOT_CN_API_KEY"},
	"zai":                 {"ZAI_API_KEY"},
	"zai-coding-cn":       {"ZAI_API_KEY", "ZAI_CODING_CN_API_KEY"},
	"ant-ling":            {"ANT_LING_API_KEY"},
	"mistral":             {"MISTRAL_API_KEY"},
	"kimi-coding":         {"KIMI_API_KEY"},
	"minimax":             {"MINIMAX_API_KEY"},
	"minimax-cn":          {"MINIMAX_API_KEY", "MINIMAX_CN_API_KEY"},
	"vercel-ai-gateway":   {"AI_GATEWAY_API_KEY"},
	"opencode":            {"OPENCODE_API_KEY"},
	"opencode-go":         {"OPENCODE_API_KEY"},
	"xiaomi":              {"XIAOMI_API_KEY"},
	"google":              {"GEMINI_API_KEY", "GOOGLE_API_KEY"},
	"google-vertex":       {"GOOGLE_CLOUD_API_KEY"},
}

// CredentialLookup resolves a stored API key for a provider id, returning ""
// when the store holds nothing usable.
type CredentialLookup func(provider string) string

var (
	credentialMu     sync.RWMutex
	credentialLookup CredentialLookup
)

// SetCredentialLookup installs a fallback credential source consulted by
// ResolveAPIKey when no environment variable is set. The coding agent uses
// this to expose ~/.maiku/agent/auth.json without making this package depend on
// the agent's on-disk layout. Passing nil clears the fallback.
func SetCredentialLookup(lookup CredentialLookup) {
	credentialMu.Lock()
	defer credentialMu.Unlock()
	credentialLookup = lookup
}

// ResolveAPIKey looks up an API key for the given provider id from known
// environment variables, then from the installed credential lookup. Returns
// "" if neither has one.
func ResolveAPIKey(provider string) string {
	for _, envVar := range providerEnvVars[provider] {
		if v := os.Getenv(envVar); v != "" {
			return v
		}
	}

	credentialMu.RLock()
	lookup := credentialLookup
	credentialMu.RUnlock()
	if lookup != nil {
		return lookup(provider)
	}
	return ""
}

// FindEnvVar returns the name of the first environment variable that is set
// and could supply an API key for the given provider, or "" if none is set.
func FindEnvVar(provider string) string {
	for _, envVar := range providerEnvVars[provider] {
		if os.Getenv(envVar) != "" {
			return envVar
		}
	}
	return ""
}

// KnownEnvVars returns a copy of the env-var map for help text.
func KnownEnvVars() map[string][]string {
	out := make(map[string][]string, len(providerEnvVars))
	for k, v := range providerEnvVars {
		out[k] = append([]string(nil), v...)
	}
	return out
}

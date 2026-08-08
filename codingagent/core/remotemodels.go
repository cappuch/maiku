package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mikus/maiku/ai"
	"github.com/mikus/maiku/ai/api/openaicodex"
	"github.com/mikus/maiku/ai/auth"
	"github.com/mikus/maiku/ai/providers"
)

var (
	remoteModelsMu sync.RWMutex
	remoteModels   = map[string][]ai.Model{}
)

// CachedRemoteModels returns the last fetched remote catalog for providerID.
func CachedRemoteModels(providerID string) []ai.Model {
	remoteModelsMu.RLock()
	defer remoteModelsMu.RUnlock()
	src := remoteModels[providerID]
	if len(src) == 0 {
		return nil
	}
	out := make([]ai.Model, len(src))
	copy(out, src)
	return out
}

// RefreshProviderModels fetches the provider's /models route and caches it.
// Failures leave the previous cache (if any) intact.
func RefreshProviderModels(ctx context.Context, providerID string) error {
	provider, ok := providers.Find(providerID)
	if !ok {
		return fmt.Errorf("unknown provider %q", providerID)
	}
	apiKey := auth.ResolveAPIKey(providerID)
	models, err := FetchProviderModels(ctx, provider, apiKey)
	if err != nil {
		return err
	}
	remoteModelsMu.Lock()
	remoteModels[providerID] = models
	remoteModelsMu.Unlock()
	return nil
}

// ProviderWithModels returns the provider metadata with its fetched catalog.
func ProviderWithModels(providerID string) (providers.Provider, bool) {
	provider, ok := providers.Find(providerID)
	if !ok {
		return providers.Provider{}, false
	}
	provider.Models = CachedRemoteModels(providerID)
	return provider, true
}

// FetchProviderModels hits the provider's models listing endpoint and maps
// entries into ai.Model values (including vision when the route exposes it).
func FetchProviderModels(ctx context.Context, provider providers.Provider, apiKey string) ([]ai.Model, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if provider.ID == "openai-codex" {
		return openaicodex.StaticModels(), nil
	}
	url, err := modelsListURL(provider, apiKey)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("accept", "application/json")
	setModelsAuthHeaders(req, provider.ID, apiKey)

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet := strings.TrimSpace(string(body))
		if len(snippet) > 240 {
			snippet = snippet[:240] + "…"
		}
		return nil, fmt.Errorf("models route %s returned %d: %s", url, resp.StatusCode, snippet)
	}

	return parseModelsResponse(provider, body)
}

func modelsListURL(provider providers.Provider, apiKey string) (string, error) {
	base := strings.TrimRight(provider.BaseURL, "/")
	if base == "" {
		return "", fmt.Errorf("provider %q has no base URL", provider.ID)
	}
	switch provider.ID {
	case "anthropic":
		return base + "/v1/models", nil
	case "google":
		u := base + "/models"
		if apiKey != "" {
			u += "?key=" + apiKey + "&pageSize=1000"
		}
		return u, nil
	default:
		return base + "/models", nil
	}
}

func setModelsAuthHeaders(req *http.Request, providerID, apiKey string) {
	if apiKey == "" {
		return
	}
	switch providerID {
	case "anthropic":
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	case "google":
		// key is query param
	default:
		req.Header.Set("authorization", "Bearer "+apiKey)
	}
}

func parseModelsResponse(provider providers.Provider, body []byte) ([]ai.Model, error) {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, err
	}

	var items []any
	switch {
	case root["data"] != nil:
		items, _ = root["data"].([]any)
	case root["models"] != nil:
		items, _ = root["models"].([]any)
	default:
		var arr []any
		if err := json.Unmarshal(body, &arr); err == nil {
			items = arr
		}
	}
	if len(items) == 0 {
		return nil, nil
	}

	out := make([]ai.Model, 0, len(items))
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		m, ok := mapRemoteModel(provider, item)
		if !ok {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

func mapRemoteModel(provider providers.Provider, item map[string]any) (ai.Model, bool) {
	id := stringField(item, "id")
	if id == "" {
		name := stringField(item, "name")
		id = strings.TrimPrefix(name, "models/")
	}
	if id == "" {
		return ai.Model{}, false
	}

	lower := strings.ToLower(id)
	if strings.Contains(lower, "embedding") ||
		strings.Contains(lower, "tts") ||
		strings.Contains(lower, "whisper") ||
		strings.Contains(lower, "moderation") ||
		strings.Contains(lower, "dall-e") ||
		strings.Contains(lower, "realtime") {
		return ai.Model{}, false
	}
	if methods, ok := item["supportedGenerationMethods"].([]any); ok {
		okGen := false
		for _, m := range methods {
			if s, _ := m.(string); s == "generateContent" || s == "streamGenerateContent" {
				okGen = true
				break
			}
		}
		if !okGen {
			return ai.Model{}, false
		}
	}

	display := stringField(item, "name", "display_name", "displayName")
	if display == "" || display == "models/"+id {
		display = id
	}
	display = strings.TrimPrefix(display, "models/")

	input := detectVisionInput(item)
	if provider.ID == "anthropic" && len(input) == 1 {
		input = []string{"text", "image"}
	}

	reasoning := detectReasoning(item)
	ctxWindow := intField(item, "context_length", "context_window", "contextWindow")
	if ctxWindow == 0 {
		if top, ok := item["top_provider"].(map[string]any); ok {
			ctxWindow = intField(top, "context_length")
		}
	}
	if ctxWindow == 0 {
		ctxWindow = intField(item, "inputTokenLimit")
	}
	maxTokens := intField(item, "max_tokens", "maxTokens", "outputTokenLimit")
	if maxTokens == 0 {
		if top, ok := item["top_provider"].(map[string]any); ok {
			maxTokens = intField(top, "max_completion_tokens")
		}
	}
	if ctxWindow == 0 {
		ctxWindow = 128000
	}
	if maxTokens == 0 {
		maxTokens = 8192
	}

	api := provider.API
	if api == "" {
		api = ai.APIOpenAICompletions
	}
	// OpenAI: prefer Responses for gpt-5 / o-series, Completions otherwise.
	if provider.ID == "openai" {
		lid := strings.ToLower(id)
		if strings.HasPrefix(lid, "gpt-5") || strings.HasPrefix(lid, "o1") ||
			strings.HasPrefix(lid, "o3") || strings.HasPrefix(lid, "o4") {
			api = ai.APIOpenAIResponses
		} else {
			api = ai.APIOpenAICompletions
		}
	}

	return ai.Model{
		ID:            id,
		Name:          display,
		API:           api,
		Provider:      provider.ID,
		BaseURL:       provider.BaseURL,
		Reasoning:     reasoning,
		Input:         input,
		Cost:          parseRemoteCost(item),
		ContextWindow: ctxWindow,
		MaxTokens:     maxTokens,
	}, true
}

func detectVisionInput(item map[string]any) []string {
	input := []string{"text"}
	hasImage := false

	if arch, ok := item["architecture"].(map[string]any); ok {
		if mods, ok := arch["input_modalities"].([]any); ok {
			for _, m := range mods {
				if s, _ := m.(string); strings.EqualFold(s, "image") {
					hasImage = true
				}
			}
		}
		if modality, _ := arch["modality"].(string); strings.Contains(strings.ToLower(modality), "image") {
			hasImage = true
		}
	}
	if mods, ok := item["input_modalities"].([]any); ok {
		for _, m := range mods {
			if s, _ := m.(string); strings.EqualFold(s, "image") {
				hasImage = true
			}
		}
	}
	if tags, ok := item["tags"].([]any); ok {
		for _, t := range tags {
			if s, _ := t.(string); strings.EqualFold(s, "vision") {
				hasImage = true
			}
		}
	}
	id := strings.ToLower(stringField(item, "id") + " " + stringField(item, "name"))
	if strings.Contains(id, "vision") || strings.Contains(id, "gpt-4o") || strings.Contains(id, "gemini") {
		hasImage = true
	}

	if hasImage {
		input = append(input, "image")
	}
	return input
}

func detectReasoning(item map[string]any) bool {
	if params, ok := item["supported_parameters"].([]any); ok {
		for _, p := range params {
			if s, _ := p.(string); s == "reasoning" || s == "thinking" {
				return true
			}
		}
	}
	if tags, ok := item["tags"].([]any); ok {
		for _, t := range tags {
			if s, _ := t.(string); s == "reasoning" || s == "thinking" {
				return true
			}
		}
	}
	id := strings.ToLower(stringField(item, "id"))
	return strings.Contains(id, "o1") || strings.Contains(id, "o3") || strings.Contains(id, "o4") ||
		strings.Contains(id, "reasoning") || strings.Contains(id, "thinking") ||
		strings.Contains(id, "gpt-5") || strings.Contains(id, "claude-opus") ||
		strings.Contains(id, "claude-sonnet-4")
}

func parseRemoteCost(item map[string]any) ai.ModelCost {
	pricing, _ := item["pricing"].(map[string]any)
	if pricing == nil {
		return ai.ModelCost{}
	}
	prompt := floatField(pricing, "prompt", "input")
	completion := floatField(pricing, "completion", "output")
	cacheRead := floatField(pricing, "input_cache_read", "cache_read")
	cacheWrite := floatField(pricing, "input_cache_write", "cache_write")

	scale := 1.0
	if prompt > 0 && prompt < 0.01 {
		scale = 1_000_000
	}
	return ai.ModelCost{
		ModelCostRates: ai.ModelCostRates{
			Input:      round4(prompt * scale),
			Output:     round4(completion * scale),
			CacheRead:  round4(cacheRead * scale),
			CacheWrite: round4(cacheWrite * scale),
		},
	}
}

func stringField(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if t, ok := v.(string); ok && t != "" {
				return t
			}
		}
	}
	return ""
}

func intField(m map[string]any, keys ...string) int {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch t := v.(type) {
			case float64:
				return int(t)
			case int:
				return t
			case json.Number:
				n, _ := t.Int64()
				return int(n)
			case string:
				n, _ := strconv.Atoi(t)
				return n
			}
		}
	}
	return 0
}

func floatField(m map[string]any, keys ...string) float64 {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch t := v.(type) {
			case float64:
				return t
			case int:
				return float64(t)
			case json.Number:
				n, _ := t.Float64()
				return n
			case string:
				n, _ := strconv.ParseFloat(t, 64)
				return n
			}
		}
	}
	return 0
}

func round4(v float64) float64 {
	return float64(int(v*10000+0.5)) / 10000
}

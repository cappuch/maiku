package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mikus/maiku/ai/providers"
)

func TestParseModelsResponseOpenRouterVision(t *testing.T) {
	body := []byte(`{
		"data": [{
			"id": "openai/gpt-4o",
			"name": "GPT-4o",
			"architecture": { "modality": "text+image->text", "input_modalities": ["text","image"] },
			"supported_parameters": ["tools","reasoning"],
			"context_length": 128000,
			"pricing": { "prompt": "0.0000025", "completion": "0.00001" }
		}]
	}`)
	provider := providers.Provider{ID: "openrouter", BaseURL: "https://openrouter.ai/api/v1", API: "openai-completions"}
	models, err := parseModelsResponse(provider, body)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 {
		t.Fatalf("len=%d", len(models))
	}
	m := models[0]
	if !contains(m.Input, "image") {
		t.Fatalf("expected vision: %+v", m.Input)
	}
	if !m.Reasoning {
		t.Fatal("expected reasoning")
	}
	if m.ContextWindow != 128000 {
		t.Fatalf("context=%d", m.ContextWindow)
	}
	if m.Cost.Input <= 0 || m.Cost.Output <= 0 {
		t.Fatalf("cost not scaled: %+v", m.Cost)
	}
	if m.Provider != "openrouter" || m.BaseURL == "" {
		t.Fatalf("provider wiring: %+v", m)
	}
}

func TestParseModelsResponseGoogle(t *testing.T) {
	body := []byte(`{
		"models": [{
			"name": "models/gemini-2.0-flash",
			"displayName": "Gemini 2.0 Flash",
			"supportedGenerationMethods": ["generateContent"],
			"inputTokenLimit": 1048576,
			"outputTokenLimit": 8192
		}, {
			"name": "models/text-embedding-004",
			"supportedGenerationMethods": ["embedContent"]
		}]
	}`)
	provider := providers.Provider{ID: "google", BaseURL: "https://generativelanguage.googleapis.com/v1beta", API: "google-generative-ai"}
	models, err := parseModelsResponse(provider, body)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 {
		t.Fatalf("len=%d want 1 (embedding skipped)", len(models))
	}
	if models[0].ID != "gemini-2.0-flash" {
		t.Fatalf("id=%q", models[0].ID)
	}
	if !contains(models[0].Input, "image") {
		t.Fatalf("gemini should detect vision heuristically: %+v", models[0].Input)
	}
}

func TestSetDefaultModelPersists(t *testing.T) {
	dir := t.TempDir()
	if err := SetDefaultModel(dir, "openrouter", "openai/gpt-4o"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "settings.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if raw["defaultProvider"] != "openrouter" || raw["defaultModel"] != "openai/gpt-4o" {
		t.Fatalf("unexpected: %#v", raw)
	}
}

func TestCachedRemoteModelsRoundTrip(t *testing.T) {
	provider := providers.Provider{ID: "openrouter", BaseURL: "https://openrouter.ai/api/v1", API: "openai-completions"}
	body := []byte(`{"data":[{"id":"acme/one","name":"One","architecture":{"input_modalities":["text"]}}]}`)
	models, err := parseModelsResponse(provider, body)
	if err != nil {
		t.Fatal(err)
	}
	remoteModelsMu.Lock()
	remoteModels["openrouter"] = models
	remoteModelsMu.Unlock()
	t.Cleanup(func() {
		remoteModelsMu.Lock()
		delete(remoteModels, "openrouter")
		remoteModelsMu.Unlock()
	})

	got := CachedRemoteModels("openrouter")
	if len(got) != 1 || got[0].ID != "acme/one" {
		t.Fatalf("cache: %+v", got)
	}
	p, ok := ProviderWithModels("openrouter")
	if !ok || len(p.Models) != 1 {
		t.Fatalf("provider models: %+v", p.Models)
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

package core

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cappuch/maiku/ai"
	"github.com/cappuch/maiku/ai/providers"
	"github.com/cappuch/maiku/codingagent"
)

func init() {
	ai.SetV1FallbackPolicy(customProviderAllowsV1)
	ai.SetBaseURLCorrectedHook(func(providerID, baseURL string) {
		_ = adoptCustomProviderBaseURL("", providerID, baseURL)
	})
}

var customProviderIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,63}$`)

// LoadCustomProviders returns custom providers from global settings.
func LoadCustomProviders(agentDir string) []CustomProvider {
	if agentDir == "" {
		agentDir = codingagent.GetAgentDir()
	}
	return LoadSettings("", agentDir).Settings.CustomProviders
}

// UpsertCustomProvider creates or updates a custom OpenAI-compatible route.
func UpsertCustomProvider(agentDir string, provider CustomProvider) error {
	previousID := strings.ToLower(strings.TrimSpace(provider.PreviousID))
	provider.PreviousID = ""
	provider.APIKey = ""
	provider.ID = strings.ToLower(strings.TrimSpace(provider.ID))
	provider.Name = strings.TrimSpace(provider.Name)
	provider.BaseURL = strings.TrimRight(strings.TrimSpace(provider.BaseURL), "/")
	provider.API = strings.TrimSpace(provider.API)
	if provider.API == "" {
		provider.API = ai.APIOpenAICompletions
	}
	if provider.ID == "" {
		return fmt.Errorf("provider id is required")
	}
	if !customProviderIDPattern.MatchString(provider.ID) {
		return fmt.Errorf("provider id must be lowercase letters/digits/_/- (2–64 chars)")
	}
	if _, builtin := providers.Find(provider.ID); builtin {
		return fmt.Errorf("provider id %q is reserved", provider.ID)
	}
	if provider.Name == "" {
		provider.Name = provider.ID
	}
	if provider.BaseURL == "" {
		return fmt.Errorf("base URL is required")
	}
	if !strings.HasPrefix(provider.BaseURL, "http://") && !strings.HasPrefix(provider.BaseURL, "https://") {
		return fmt.Errorf("base URL must start with http:// or https://")
	}
	ai.ForgetCorrectedBaseURL(provider.ID)
	if previousID != "" {
		ai.ForgetCorrectedBaseURL(previousID)
	}

	list := LoadCustomProviders(agentDir)
	if previousID != "" && previousID != provider.ID {
		found := false
		for _, existing := range list {
			if existing.ID == provider.ID {
				return fmt.Errorf("provider id %q already exists", provider.ID)
			}
		}
		for i, existing := range list {
			if existing.ID == previousID {
				list[i] = provider
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("unknown custom provider %q", previousID)
		}
	} else {
		found := false
		for i, existing := range list {
			if existing.ID == provider.ID {
				list[i] = provider
				found = true
				break
			}
		}
		if !found {
			list = append(list, provider)
		}
	}
	return PatchGlobalSettings(agentDir, map[string]any{"customProviders": list})
}

// ApplyCustomProviderCredential stores an edited API key. A blank key keeps
// the current credential and moves it when the provider id changes.
func ApplyCustomProviderCredential(store *AuthStorage, previousID, id, apiKey string) error {
	if store == nil {
		return nil
	}
	previousID = strings.ToLower(strings.TrimSpace(previousID))
	id = strings.ToLower(strings.TrimSpace(id))
	apiKey = strings.TrimSpace(apiKey)
	if id == "" {
		return fmt.Errorf("provider id is required")
	}
	if apiKey != "" {
		if err := store.Write(id, Credential{Type: CredentialAPIKey, Key: apiKey}); err != nil {
			return err
		}
	} else if previousID != "" && previousID != id {
		if cred, ok := store.Read(previousID); ok {
			if err := store.Write(id, cred); err != nil {
				return err
			}
		}
	}
	if previousID != "" && previousID != id {
		return store.Delete(previousID)
	}
	return nil
}

// RemoveCustomProvider deletes a custom provider from settings.
func RemoveCustomProvider(agentDir, id string) error {
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" {
		return fmt.Errorf("provider id is required")
	}
	ai.ForgetCorrectedBaseURL(id)
	list := LoadCustomProviders(agentDir)
	out := make([]CustomProvider, 0, len(list))
	found := false
	for _, existing := range list {
		if existing.ID == id {
			found = true
			continue
		}
		out = append(out, existing)
	}
	if !found {
		return fmt.Errorf("unknown custom provider %q", id)
	}
	return PatchGlobalSettings(agentDir, map[string]any{"customProviders": out})
}

func customProviderAllowsV1(providerID, baseURL string) bool {
	if _, ok := ai.BaseURLWithV1(baseURL); !ok {
		return false
	}
	for _, provider := range LoadCustomProviders("") {
		if provider.ID != providerID {
			continue
		}
		api := provider.API
		if api == "" {
			api = ai.APIOpenAICompletions
		}
		return api == ai.APIOpenAICompletions || api == ai.APIOpenAIResponses
	}
	return false
}

// adoptCustomProviderBaseURL stores a base URL that only worked after /v1
// was appended. It leaves the saved route alone when the user has since
// edited it to something else.
func adoptCustomProviderBaseURL(agentDir, id, baseURL string) error {
	id = strings.ToLower(strings.TrimSpace(id))
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if id == "" || baseURL == "" {
		return nil
	}
	list := LoadCustomProviders(agentDir)
	updated := false
	for i, existing := range list {
		if existing.ID != id {
			continue
		}
		current := strings.TrimRight(existing.BaseURL, "/")
		if current == baseURL {
			break
		}
		fixed, ok := ai.BaseURLWithV1(current)
		if !ok || fixed != baseURL {
			return nil
		}
		list[i].BaseURL = baseURL
		updated = true
		break
	}
	if updated {
		if err := PatchGlobalSettings(agentDir, map[string]any{"customProviders": list}); err != nil {
			return err
		}
	}
	remoteModelsMu.Lock()
	if models, ok := remoteModels[id]; ok {
		for i := range models {
			models[i].BaseURL = baseURL
		}
	}
	remoteModelsMu.Unlock()
	return nil
}

// CustomProviderAsRegistry converts a custom provider into registry metadata.
func CustomProviderAsRegistry(provider CustomProvider) providers.Provider {
	api := provider.API
	if api == "" {
		api = ai.APIOpenAICompletions
	}
	return providers.Provider{
		ID:      provider.ID,
		Name:    provider.Name,
		BaseURL: provider.BaseURL,
		API:     api,
	}
}

// StaticModelsFromCustom builds ai.Model entries from configured model ids.
func StaticModelsFromCustom(provider CustomProvider) []ai.Model {
	api := provider.API
	if api == "" {
		api = ai.APIOpenAICompletions
	}
	out := make([]ai.Model, 0, len(provider.Models))
	for _, id := range provider.Models {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		out = append(out, ai.Model{
			ID:            id,
			Name:          id,
			API:           api,
			Provider:      provider.ID,
			BaseURL:       provider.BaseURL,
			Reasoning:     true,
			ContextWindow: 128000,
			MaxTokens:     8192,
		})
	}
	return out
}

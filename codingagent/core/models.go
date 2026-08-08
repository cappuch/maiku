package core

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mikus/maiku/ai"
	"github.com/mikus/maiku/ai/auth"
	"github.com/mikus/maiku/ai/providers"
)

// DefaultProviderID is the provider used when the user gives no hints and
// several API keys are present.
const DefaultProviderID = "anthropic"

// DefaultModelID is the model used when the default provider is available.
const DefaultModelID = "claude-sonnet-4-5"

// AllProviders returns the built-in provider catalog in preference order.
func AllProviders() []providers.Provider {
	return providers.All()
}

// AllModels returns every model in the built-in catalog.
func AllModels() []ai.Model {
	var models []ai.Model
	for _, provider := range AllProviders() {
		models = append(models, provider.Models...)
	}
	return models
}

// FindProvider returns the provider with the given id.
func FindProvider(id string) (providers.Provider, bool) {
	for _, provider := range AllProviders() {
		if provider.ID == id {
			return provider, true
		}
	}
	return providers.Provider{}, false
}

// HasAPIKey reports whether an API key for the provider is available from the
// environment.
func HasAPIKey(providerID string) bool {
	return auth.ResolveAPIKey(providerID) != ""
}

// ResolveModelOptions describes the user's model selection inputs.
type ResolveModelOptions struct {
	// Provider is the --provider value, if any.
	Provider string
	// Model is the --model value: an exact id, a "provider/id" pair, or a
	// substring to match.
	Model string
	// RequireAPIKey restricts fallback selection to providers that have a
	// usable API key.
	RequireAPIKey bool
}

// ResolveModel picks a model from the built-in catalog.
//
// Resolution order: explicit provider+model, "provider/id", exact model id,
// substring match, then the default model for whichever provider has an API
// key available.
func ResolveModel(options ResolveModelOptions) (ai.Model, error) {
	candidates := AllModels()
	if options.Provider != "" {
		provider, ok := FindProvider(options.Provider)
		if !ok {
			return ai.Model{}, fmt.Errorf("unknown provider %q (known: %s)", options.Provider, strings.Join(providerIDs(), ", "))
		}
		candidates = provider.Models
	}

	spec := strings.TrimSpace(options.Model)
	if spec == "" {
		return defaultModel(options.Provider, options.RequireAPIKey)
	}

	// Exact id match first: OpenRouter ids such as "anthropic/claude-sonnet-4.5"
	// contain a slash themselves, so this must be tried before splitting.
	for _, model := range candidates {
		if model.ID == spec {
			return model, nil
		}
	}

	if providerID, rest, ok := strings.Cut(spec, "/"); ok && options.Provider == "" {
		if provider, found := FindProvider(providerID); found {
			for _, model := range provider.Models {
				if model.ID == rest {
					return model, nil
				}
			}
			for _, model := range provider.Models {
				if strings.Contains(strings.ToLower(model.ID), strings.ToLower(rest)) {
					return model, nil
				}
			}
			return ai.Model{}, fmt.Errorf("provider %q has no model matching %q", providerID, rest)
		}
	}

	lower := strings.ToLower(spec)
	// Ignore very short substring queries (e.g. settings leftover "x") — they
	// match almost every provider id and produce confusing results.
	if len(lower) >= 3 {
		for _, model := range candidates {
			if strings.Contains(strings.ToLower(model.ID), lower) || strings.Contains(strings.ToLower(model.Name), lower) {
				return model, nil
			}
		}
	}

	return ai.Model{}, fmt.Errorf("no model matching %q (try --list-models)", spec)
}

func defaultModel(providerID string, requireAPIKey bool) (ai.Model, error) {
	if providerID != "" {
		provider, ok := FindProvider(providerID)
		if !ok {
			return ai.Model{}, fmt.Errorf("unknown provider %q (known: %s)", providerID, strings.Join(providerIDs(), ", "))
		}
		if len(provider.Models) == 0 {
			return ai.Model{}, fmt.Errorf("provider %q has no models", providerID)
		}
		return provider.Models[0], nil
	}

	if !requireAPIKey {
		if model, err := ResolveModel(ResolveModelOptions{Provider: DefaultProviderID, Model: DefaultModelID}); err == nil {
			return model, nil
		}
	}

	if HasAPIKey(DefaultProviderID) {
		if model, err := ResolveModel(ResolveModelOptions{Provider: DefaultProviderID, Model: DefaultModelID}); err == nil {
			return model, nil
		}
	}
	for _, provider := range AllProviders() {
		if HasAPIKey(provider.ID) && len(provider.Models) > 0 {
			return provider.Models[0], nil
		}
	}

	return ai.Model{}, fmt.Errorf(
		"no API key found. Set a provider key (e.g. ANTHROPIC_API_KEY, OPENAI_API_KEY, GEMINI_API_KEY, OPENROUTER_API_KEY) or store credentials in ~/.maiku/agent/auth.json",
	)
}

func providerIDs() []string {
	var ids []string
	for _, provider := range AllProviders() {
		ids = append(ids, provider.ID)
	}
	sort.Strings(ids)
	return ids
}

// FormatModelList renders the catalog for --list-models, optionally filtered
// by a case-insensitive substring.
func FormatModelList(search string) string {
	var b strings.Builder
	lower := strings.ToLower(search)

	for _, provider := range AllProviders() {
		var matched []ai.Model
		for _, model := range provider.Models {
			if search == "" ||
				strings.Contains(strings.ToLower(model.ID), lower) ||
				strings.Contains(strings.ToLower(model.Name), lower) ||
				strings.Contains(strings.ToLower(provider.ID), lower) {
				matched = append(matched, model)
			}
		}
		if len(matched) == 0 {
			continue
		}

		keyState := "no API key"
		if envVar := auth.FindEnvVar(provider.ID); envVar != "" {
			keyState = envVar
		}
		fmt.Fprintf(&b, "%s (%s)\n", provider.Name, keyState)
		for _, model := range matched {
			reasoning := ""
			if model.Reasoning {
				reasoning = " [reasoning]"
			}
			fmt.Fprintf(&b, "  %s/%s%s\n", provider.ID, model.ID, reasoning)
		}
		b.WriteString("\n")
	}

	if b.Len() == 0 {
		return fmt.Sprintf("No models matching %q\n", search)
	}
	return b.String()
}

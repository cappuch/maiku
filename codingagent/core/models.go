package core

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/mikus/maiku/ai"
	"github.com/mikus/maiku/ai/auth"
	"github.com/mikus/maiku/ai/providers"
)

// DefaultProviderID is the provider used when the user gives no hints and
// several API keys are present.
const DefaultProviderID = "anthropic"

// AllProviders returns known providers with their fetched model catalogs.
func AllProviders() []providers.Provider {
	builtin := providers.All()
	out := make([]providers.Provider, len(builtin))
	for i, p := range builtin {
		out[i] = p
		out[i].Models = CachedRemoteModels(p.ID)
	}
	return out
}

// AllModels returns every fetched model across providers that have been refreshed.
func AllModels() []ai.Model {
	var models []ai.Model
	for _, provider := range AllProviders() {
		models = append(models, provider.Models...)
	}
	return models
}

// FindProvider returns the provider with the given id and its fetched models.
func FindProvider(id string) (providers.Provider, bool) {
	return ProviderWithModels(id)
}

// HasAPIKey reports whether an API key for the provider is available.
func HasAPIKey(providerID string) bool {
	return auth.ResolveAPIKey(providerID) != ""
}

// ResolveModelOptions describes the user's model selection inputs.
type ResolveModelOptions struct {
	Provider      string
	Model         string
	RequireAPIKey bool
}

// ResolveModel picks a model from the fetched catalog.
//
// Call RefreshProviderModels first for the relevant provider(s). Without a
// successful fetch the catalog is empty and resolution fails.
func ResolveModel(options ResolveModelOptions) (ai.Model, error) {
	candidates := AllModels()
	if options.Provider != "" {
		provider, ok := FindProvider(options.Provider)
		if !ok {
			return ai.Model{}, fmt.Errorf("unknown provider %q (known: %s)", options.Provider, strings.Join(providerIDs(), ", "))
		}
		candidates = provider.Models
		if len(candidates) == 0 {
			return ai.Model{}, fmt.Errorf("provider %q has no models yet — add an API key and refresh the models list", options.Provider)
		}
	}

	spec := strings.TrimSpace(options.Model)
	if spec == "" {
		return defaultModel(options.Provider, options.RequireAPIKey)
	}

	for _, model := range candidates {
		if model.ID == spec {
			return model, nil
		}
	}

	if providerID, rest, ok := strings.Cut(spec, "/"); ok && options.Provider == "" {
		if provider, found := FindProvider(providerID); found {
			if len(provider.Models) == 0 {
				return ai.Model{}, fmt.Errorf("provider %q has no models yet — add an API key and refresh the models list", providerID)
			}
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
	if len(lower) >= 3 {
		for _, model := range candidates {
			if strings.Contains(strings.ToLower(model.ID), lower) || strings.Contains(strings.ToLower(model.Name), lower) {
				return model, nil
			}
		}
	}

	if len(candidates) == 0 {
		return ai.Model{}, fmt.Errorf("no models loaded — configure a provider API key and refresh (try --list-models)")
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
			return ai.Model{}, fmt.Errorf("provider %q has no models yet — add an API key and refresh the models list", providerID)
		}
		return provider.Models[0], nil
	}

	try := func(id string) (ai.Model, bool) {
		if requireAPIKey && !HasAPIKey(id) {
			return ai.Model{}, false
		}
		p, ok := FindProvider(id)
		if !ok || len(p.Models) == 0 {
			return ai.Model{}, false
		}
		return p.Models[0], true
	}

	if m, ok := try(DefaultProviderID); ok {
		return m, nil
	}
	for _, provider := range AllProviders() {
		if requireAPIKey && !HasAPIKey(provider.ID) {
			continue
		}
		if len(provider.Models) > 0 {
			return provider.Models[0], nil
		}
	}

	if requireAPIKey {
		return ai.Model{}, fmt.Errorf(
			"no API key found. Set a provider key (e.g. ANTHROPIC_API_KEY, OPENAI_API_KEY, GEMINI_API_KEY, OPENROUTER_API_KEY) or store credentials in ~/.maiku/agent/auth.json",
		)
	}
	return ai.Model{}, fmt.Errorf("no models loaded — configure a provider API key and refresh (try --list-models)")
}

func providerIDs() []string {
	allProviders := providers.All()
	ids := make([]string, 0, len(allProviders))
	for _, provider := range allProviders {
		ids = append(ids, provider.ID)
	}
	sort.Strings(ids)
	return ids
}

// FormatModelList renders the fetched catalog for --list-models.
func FormatModelList(search string) string {
	var b strings.Builder
	lower := strings.ToLower(search)
	anyProvider := false

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
		anyProvider = true

		keyState := "no API key"
		if envVar := auth.FindEnvVar(provider.ID); envVar != "" {
			keyState = envVar
		} else if HasAPIKey(provider.ID) {
			keyState = "key configured"
		}
		fmt.Fprintf(&b, "%s (%s)\n", provider.Name, keyState)
		for _, model := range matched {
			reasoning := ""
			if model.Reasoning {
				reasoning = " [reasoning]"
			}
			vision := ""
			if slices.Contains(model.Input, "image") {
				vision = " [vision]"
			}
			fmt.Fprintf(&b, "  %s/%s%s%s\n", provider.ID, model.ID, reasoning, vision)
		}
		b.WriteString("\n")
	}

	if !anyProvider {
		if search != "" {
			return fmt.Sprintf("No models matching %q\n", search)
		}
		return "No models loaded. Configure a provider API key, then re-run --list-models.\n"
	}
	return b.String()
}

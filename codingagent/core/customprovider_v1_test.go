package core

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cappuch/maiku/ai"
)

func TestCustomProviderModels404RewritesBaseURL(t *testing.T) {
	t.Setenv("MAIKU_AGENT_DIR", t.TempDir())
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/v1/models" {
			w.Header().Set("content-type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"demo","name":"Demo"}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	if err := UpsertCustomProvider("", CustomProvider{
		ID: "acme", Name: "Acme", BaseURL: srv.URL, API: ai.APIOpenAICompletions,
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ai.ForgetCorrectedBaseURL("acme")
		remoteModelsMu.Lock()
		delete(remoteModels, "acme")
		remoteModelsMu.Unlock()
	})

	provider := CustomProviderAsRegistry(LoadCustomProviders("")[0])
	models, err := FetchProviderModels(context.Background(), provider, "test-key")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID != "demo" || models[0].BaseURL != srv.URL+"/v1" {
		t.Fatalf("models = %+v", models)
	}
	saved := LoadCustomProviders("")
	if len(saved) != 1 || saved[0].BaseURL != srv.URL+"/v1" {
		t.Fatalf("stored base = %+v", saved)
	}
	if paths[0] != "/models" || paths[1] != "/v1/models" {
		t.Fatalf("paths = %v", paths)
	}
}

func TestCustomProviderAuthFailureDoesNotRewriteBaseURL(t *testing.T) {
	t.Setenv("MAIKU_AGENT_DIR", t.TempDir())
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer srv.Close()
	if err := UpsertCustomProvider("", CustomProvider{
		ID: "locked", Name: "Locked", BaseURL: srv.URL,
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ai.ForgetCorrectedBaseURL("locked") })

	provider := CustomProviderAsRegistry(LoadCustomProviders("")[0])
	if _, err := FetchProviderModels(context.Background(), provider, ""); err == nil {
		t.Fatal("expected unauthorized models route to fail")
	}
	saved := LoadCustomProviders("")
	if saved[0].BaseURL != srv.URL {
		t.Fatalf("base rewritten on auth failure: %q", saved[0].BaseURL)
	}
	if len(paths) != 1 || paths[0] != "/models" {
		t.Fatalf("paths = %v", paths)
	}
}

func TestCustomProviderWithV1IsNotDoubled(t *testing.T) {
	t.Setenv("MAIKU_AGENT_DIR", t.TempDir())
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/v1/models" {
			_, _ = w.Write([]byte(`{"data":[{"id":"demo"}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	base := srv.URL + "/v1"
	if err := UpsertCustomProvider("", CustomProvider{ID: "ready", BaseURL: base}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ai.ForgetCorrectedBaseURL("ready") })
	provider := CustomProviderAsRegistry(LoadCustomProviders("")[0])
	models, err := FetchProviderModels(context.Background(), provider, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].BaseURL != base {
		t.Fatalf("models = %+v", models)
	}
	if len(paths) != 1 || paths[0] != "/v1/models" {
		t.Fatalf("paths = %v", paths)
	}
}

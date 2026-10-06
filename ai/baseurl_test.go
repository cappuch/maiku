package ai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBaseURLWithV1(t *testing.T) {
	fixed, ok := BaseURLWithV1("https://example.com")
	if !ok || fixed != "https://example.com/v1" {
		t.Fatalf("bare host: %q ok=%v", fixed, ok)
	}
	fixed, ok = BaseURLWithV1("https://example.com/")
	if !ok || fixed != "https://example.com/v1" {
		t.Fatalf("trailing slash: %q ok=%v", fixed, ok)
	}
	fixed, ok = BaseURLWithV1("https://example.com/openai")
	if !ok || fixed != "https://example.com/openai/v1" {
		t.Fatalf("path: %q ok=%v", fixed, ok)
	}
	if _, ok := BaseURLWithV1("https://example.com/v1"); ok {
		t.Fatal("already /v1 should not be rewritten")
	}
	if _, ok := BaseURLWithV1("https://example.com/V1/"); ok {
		t.Fatal("already /V1 should not be rewritten")
	}
	if _, ok := BaseURLWithV1("example.com"); ok {
		t.Fatal("missing scheme should not be rewritten")
	}
}

func TestPromoteOpenAIV1Retries404(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/v1/chat/completions" {
			w.Header().Set("content-type", "text/event-stream")
			_, _ = io.WriteString(w, "data: ok\n")
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	SetV1FallbackPolicy(func(providerID, baseURL string) bool {
		return providerID == "acme"
	})
	noted := ""
	SetBaseURLCorrectedHook(func(providerID, baseURL string) {
		noted = providerID + " " + baseURL
	})
	t.Cleanup(func() {
		SetV1FallbackPolicy(nil)
		SetBaseURLCorrectedHook(nil)
		ForgetCorrectedBaseURL("acme")
	})

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/chat/completions", strings.NewReader(`{"model":"demo"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("authorization", "Bearer secret")
	client := srv.Client()
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"model":"demo"}`)
	next, base := PromoteOpenAIV1(client, resp, HTTPRetryPolicy{MaxRetries: 0}, "acme", srv.URL, "/chat/completions", body)
	defer func() { _ = next.Body.Close() }()
	if base != srv.URL+"/v1" {
		t.Fatalf("base = %q", base)
	}
	if next.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", next.StatusCode)
	}
	if noted != "acme "+srv.URL+"/v1" {
		t.Fatalf("noted = %q", noted)
	}
	if UseCorrectedBaseURL("acme") != srv.URL+"/v1" {
		t.Fatalf("cache = %q", UseCorrectedBaseURL("acme"))
	}
	got, _ := io.ReadAll(next.Body)
	if !strings.Contains(string(got), "ok") {
		t.Fatalf("body = %q", got)
	}
	if len(paths) != 2 || paths[0] != "/chat/completions" || paths[1] != "/v1/chat/completions" {
		t.Fatalf("paths = %v", paths)
	}
}

func TestPromoteOpenAIV1LeavesOtherFailures(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer srv.Close()
	SetV1FallbackPolicy(func(providerID, baseURL string) bool { return true })
	t.Cleanup(func() { SetV1FallbackPolicy(nil) })

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/models", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	next, base := PromoteOpenAIV1(srv.Client(), resp, HTTPRetryPolicy{}, "acme", srv.URL, "/models", nil)
	defer func() { _ = next.Body.Close() }()
	if base != srv.URL || next.StatusCode != http.StatusUnauthorized {
		t.Fatalf("base=%q status=%d", base, next.StatusCode)
	}
	if len(paths) != 1 {
		t.Fatalf("retried a non-404: %v", paths)
	}
}

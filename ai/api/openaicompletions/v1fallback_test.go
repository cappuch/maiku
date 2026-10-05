package openaicompletions

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cappuch/maiku/ai"
)

func TestStreamRetriesBareBaseWithV1(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("content-type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"id\":\"1\",\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":null}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"id\":\"1\",\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	ai.SetV1FallbackPolicy(func(providerID, baseURL string) bool {
		return providerID == "acme"
	})
	t.Cleanup(func() {
		ai.SetV1FallbackPolicy(nil)
		ai.SetBaseURLCorrectedHook(nil)
		ai.ForgetCorrectedBaseURL("acme")
	})

	model := ai.Model{
		ID:       "demo",
		Provider: "acme",
		API:      ai.APIOpenAICompletions,
		BaseURL:  srv.URL,
	}
	msg := Stream(model, ai.Context{}, &ai.SimpleStreamOptions{StreamOptions: ai.StreamOptions{APIKey: "test"}}).Result()
	if msg.StopReason == ai.StopError {
		t.Fatalf("stream error: %s", msg.ErrorMessage)
	}
	if len(msg.Content) != 1 || msg.Content[0].Text != "ok" {
		t.Fatalf("content = %+v", msg.Content)
	}
	if ai.UseCorrectedBaseURL("acme") != srv.URL+"/v1" {
		t.Fatalf("corrected = %q", ai.UseCorrectedBaseURL("acme"))
	}
	if len(paths) != 2 || paths[0] != "/chat/completions" || paths[1] != "/v1/chat/completions" {
		t.Fatalf("paths = %v", paths)
	}

	paths = nil
	again := Stream(model, ai.Context{}, &ai.SimpleStreamOptions{StreamOptions: ai.StreamOptions{APIKey: "test"}}).Result()
	if again.StopReason == ai.StopError {
		t.Fatalf("second stream error: %s", again.ErrorMessage)
	}
	if len(paths) != 1 || paths[0] != "/v1/chat/completions" {
		t.Fatalf("second request did not use the corrected base: %v", paths)
	}
}

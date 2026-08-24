package openairesponses

import (
	"encoding/json"
	"testing"

	"github.com/mikus/maiku/ai"
)

func TestPromptCacheKeyFromSessionID(t *testing.T) {
	model := ai.Model{ID: "gpt-5", Provider: "openai", API: ai.APIOpenAIResponses}
	ctx := ai.Context{
		SystemPrompt: "you are maiku",
		Messages:     []ai.Message{{Role: "user", UserContent: "hello"}},
	}

	opts := &ai.SimpleStreamOptions{}
	opts.SessionID = "session-123"
	req := buildRequest(model, ctx, opts)
	if req.PromptCacheKey != "session-123" {
		t.Fatalf("expected prompt_cache_key from SessionID, got %q", req.PromptCacheKey)
	}

	// CacheNone (e.g. compaction summaries) must not pin routing: the
	// standalone summary should not evict or share the conversation shard.
	none := &ai.SimpleStreamOptions{}
	none.SessionID = "session-123"
	none.CacheRetention = ai.CacheNone
	reqNone := buildRequest(model, ctx, none)
	if reqNone.PromptCacheKey != "" {
		t.Fatalf("expected no prompt_cache_key with CacheNone, got %q", reqNone.PromptCacheKey)
	}

	// No session ID: omit the field entirely so providers keep their
	// default routing.
	reqEmpty := buildRequest(model, ctx, &ai.SimpleStreamOptions{})
	if reqEmpty.PromptCacheKey != "" {
		t.Fatalf("expected empty prompt_cache_key without SessionID, got %q", reqEmpty.PromptCacheKey)
	}

	// The field must serialize as prompt_cache_key on the wire.
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if wire["prompt_cache_key"] != "session-123" {
		t.Fatalf("expected prompt_cache_key on the wire, got %v", wire["prompt_cache_key"])
	}
}

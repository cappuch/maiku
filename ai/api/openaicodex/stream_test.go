package openaicodex

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestExtractAccountID(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{
		jwtClaimPath: map[string]any{"chatgpt_account_id": "acc_123"},
	})
	token := "aaa." + base64.RawURLEncoding.EncodeToString(payload) + ".bbb"
	id, err := extractAccountID(token)
	if err != nil {
		t.Fatal(err)
	}
	if id != "acc_123" {
		t.Fatalf("got %q", id)
	}
}

func TestResolveCodexURL(t *testing.T) {
	cases := map[string]string{
		"https://chatgpt.com/backend-api":               "https://chatgpt.com/backend-api/codex/responses",
		"https://chatgpt.com/backend-api/":              "https://chatgpt.com/backend-api/codex/responses",
		"https://chatgpt.com/backend-api/codex":         "https://chatgpt.com/backend-api/codex/responses",
		"https://chatgpt.com/backend-api/codex/responses": "https://chatgpt.com/backend-api/codex/responses",
	}
	for in, want := range cases {
		if got := resolveCodexURL(in); got != want {
			t.Errorf("resolveCodexURL(%q)=%q want %q", in, got, want)
		}
	}
}

func TestClampCodexReasoningEffort(t *testing.T) {
	if got := clampCodexReasoningEffort("gpt-5.5", "minimal"); got != "low" {
		t.Fatalf("got %q", got)
	}
	if got := clampCodexReasoningEffort("gpt-5.5", "xhigh"); got != "xhigh" {
		t.Fatalf("got %q", got)
	}
}

func TestStaticModels(t *testing.T) {
	models := StaticModels()
	if len(models) == 0 {
		t.Fatal("expected models")
	}
	found := false
	for _, m := range models {
		if m.ID == "gpt-5.5" && m.API == "openai-codex-responses" && m.Provider == "openai-codex" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing gpt-5.5")
	}
}

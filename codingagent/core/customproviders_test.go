package core

import (
	"os"
	"strings"
	"testing"

	"github.com/cappuch/maiku/ai"
)

func TestUpsertCustomProviderRenamesWithoutStoringSecrets(t *testing.T) {
	dir := t.TempDir()
	if err := UpsertCustomProvider(dir, CustomProvider{
		ID: "old-route", Name: "Old", BaseURL: "https://old.example/v1", Models: []string{"m"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := UpsertCustomProvider(dir, CustomProvider{
		PreviousID: "old-route",
		ID:         "new-route",
		Name:       "New",
		BaseURL:    "https://new.example/v1",
		API:        ai.APIOpenAIResponses,
		Models:     []string{"n"},
		APIKey:     "secret-value",
	}); err != nil {
		t.Fatal(err)
	}
	list := LoadCustomProviders(dir)
	if len(list) != 1 || list[0].ID != "new-route" || list[0].BaseURL != "https://new.example/v1" || list[0].Name != "New" || list[0].API != ai.APIOpenAIResponses {
		t.Fatalf("provider = %+v", list)
	}
	if list[0].APIKey != "" || list[0].PreviousID != "" {
		t.Fatalf("edit metadata leaked into the loaded provider: %+v", list[0])
	}
	raw, err := os.ReadFile(GlobalSettingsPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if strings.Contains(text, "secret-value") || strings.Contains(text, "previousId") || strings.Contains(text, "apiKey") || strings.Contains(text, "old-route") {
		t.Fatalf("settings stored edit secrets or the old id:\n%s", text)
	}
}

func TestApplyCustomProviderCredentialMovesKeyOnRename(t *testing.T) {
	store := NewAuthStorage(t.TempDir() + "/auth.json")
	if err := store.Write("old-route", Credential{Type: CredentialAPIKey, Key: "kept-secret"}); err != nil {
		t.Fatal(err)
	}
	if err := ApplyCustomProviderCredential(store, "old-route", "new-route", ""); err != nil {
		t.Fatal(err)
	}
	if store.APIKey("new-route") != "kept-secret" || store.APIKey("old-route") != "" {
		t.Fatalf("new=%q old=%q", store.APIKey("new-route"), store.APIKey("old-route"))
	}
	if err := ApplyCustomProviderCredential(store, "new-route", "new-route", ""); err != nil {
		t.Fatal(err)
	}
	if store.APIKey("new-route") != "kept-secret" {
		t.Fatal("blank key cleared the saved credential")
	}
	if err := ApplyCustomProviderCredential(store, "new-route", "new-route", "rotated"); err != nil {
		t.Fatal(err)
	}
	if store.APIKey("new-route") != "rotated" {
		t.Fatalf("key = %q", store.APIKey("new-route"))
	}
}

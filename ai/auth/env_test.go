package auth

import "testing"

func TestResolveAPIKeyMiru(t *testing.T) {
	t.Setenv("TAKARA_API_KEY", "takara-test-key")
	if got := ResolveAPIKey("miru"); got != "takara-test-key" {
		t.Fatalf("ResolveAPIKey(\"miru\") = %q, want Takara key", got)
	}
}

func TestResolveAPIKeyMiruUsesCredentialLookup(t *testing.T) {
	t.Setenv("TAKARA_API_KEY", "")
	SetCredentialLookup(func(provider string) string {
		if provider == "miru" {
			return "stored-takara-key"
		}
		return ""
	})
	t.Cleanup(func() { SetCredentialLookup(nil) })

	if got := ResolveAPIKey("miru"); got != "stored-takara-key" {
		t.Fatalf("ResolveAPIKey(\"miru\") = %q, want stored Takara key", got)
	}
}

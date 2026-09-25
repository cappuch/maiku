package auth

import (
	"path/filepath"
	"testing"
)

func TestBedrockCredentials(t *testing.T) {
	SetCredentialLookup(nil)
	t.Cleanup(func() { SetCredentialLookup(nil) })
	for _, key := range []string{"AWS_BEARER_TOKEN_BEDROCK", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_PROFILE", "AWS_DEFAULT_PROFILE", "AWS_WEB_IDENTITY_TOKEN_FILE", "AWS_CONTAINER_CREDENTIALS_RELATIVE_URI", "AWS_CONTAINER_CREDENTIALS_FULL_URI", "AWS_REGION", "AWS_DEFAULT_REGION"} {
		t.Setenv(key, "")
	}
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "missing-config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "missing-credentials"))
	if HasCredentials("amazon-bedrock") {
		t.Fatal("unconfigured Bedrock has credentials")
	}
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "test-token")
	if !HasCredentials("amazon-bedrock") || ResolveAPIKey("amazon-bedrock") != "test-token" || FindEnvVar("amazon-bedrock") != "AWS_BEARER_TOKEN_BEDROCK" {
		t.Fatal("bearer token not resolved")
	}
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "access")
	if HasCredentials("amazon-bedrock") {
		t.Fatal("incomplete access keys accepted")
	}
	t.Setenv("AWS_SECRET_ACCESS_KEY", "secret")
	if !HasCredentials("amazon-bedrock") || ResolveAPIKey("amazon-bedrock") != "" {
		t.Fatal("AWS credentials must not become bearer tokens")
	}
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_PROFILE", "sso-profile")
	if !HasCredentials("amazon-bedrock") {
		t.Fatal("profile not detected")
	}
	if HasCredentials("unknown-provider") {
		t.Fatal("AWS profile enabled another provider")
	}
}

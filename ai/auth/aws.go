package auth

import (
	"context"
	"os"

	"github.com/aws/aws-sdk-go-v2/config"
)

// HasCredentials reports locally configured authentication without performing
// network requests. For Bedrock, actual credential resolution and validation is
// deferred to the AWS SDK when making a request, including SSO and IAM roles.
func HasCredentials(provider string) bool {
	if ResolveAPIKey(provider) != "" {
		return true
	}
	if provider != "bedrock" && provider != "amazon-bedrock" {
		return false
	}
	if os.Getenv("AWS_ACCESS_KEY_ID") != "" && os.Getenv("AWS_SECRET_ACCESS_KEY") != "" {
		return true
	}
	for _, key := range []string{"AWS_PROFILE", "AWS_DEFAULT_PROFILE", "AWS_WEB_IDENTITY_TOKEN_FILE", "AWS_CONTAINER_CREDENTIALS_RELATIVE_URI", "AWS_CONTAINER_CREDENTIALS_FULL_URI", "AWS_REGION", "AWS_DEFAULT_REGION"} {
		if os.Getenv(key) != "" {
			return true
		}
	}
	shared, err := config.LoadSharedConfigProfile(context.Background(), "default")
	return err == nil && (shared.Credentials.HasKeys() || shared.CredentialProcess != "" || shared.RoleARN != "" || shared.SSOSession != nil || shared.SSOStartURL != "" || shared.Region != "")
}

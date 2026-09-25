// Package bedrock implements Amazon Bedrock's Converse streaming API.
package bedrock

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/cappuch/maiku/ai"
)

const ProviderID = "amazon-bedrock"

type signedTransport struct {
	config      aws.Config
	token       string
	payloadHash string
}

func (t signedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	if t.token != "" {
		req.Header.Set("Authorization", "Bearer "+t.token)
	} else {
		credentials, err := t.config.Credentials.Retrieve(req.Context())
		if err != nil {
			return nil, fmt.Errorf("Bedrock AWS credentials: %w", err)
		}
		if err := v4.NewSigner().SignHTTP(req.Context(), credentials, req, t.payloadHash, "bedrock", t.config.Region, time.Now()); err != nil {
			return nil, err
		}
	}
	return http.DefaultTransport.RoundTrip(req)
}

func loadConfig(ctx context.Context, opts *ai.SimpleStreamOptions) (aws.Config, error) {
	options := []func(*config.LoadOptions) error{config.WithDefaultRegion("us-east-1")}
	if opts != nil {
		if region := opts.Env["AWS_REGION"]; region != "" {
			options = append(options, config.WithRegion(region))
		} else if region := opts.Env["AWS_DEFAULT_REGION"]; region != "" {
			options = append(options, config.WithRegion(region))
		}
		if profile := opts.Env["AWS_PROFILE"]; profile != "" {
			options = append(options, config.WithSharedConfigProfile(profile))
		}
	}
	return config.LoadDefaultConfig(ctx, options...)
}

func endpoint(cfg aws.Config, service string) string {
	key := "AWS_ENDPOINT_URL_" + strings.ToUpper(strings.ReplaceAll(service, "-", "_"))
	if value := os.Getenv(key); value != "" {
		return strings.TrimRight(value, "/")
	}
	if value := os.Getenv("AWS_ENDPOINT_URL"); value != "" {
		return strings.TrimRight(value, "/")
	}
	suffix := "amazonaws.com"
	if strings.HasPrefix(cfg.Region, "cn-") {
		suffix = "amazonaws.com.cn"
	}
	return "https://" + service + "." + cfg.Region + "." + suffix
}

func client(cfg aws.Config, token string, body []byte) *http.Client {
	return &http.Client{
		Transport: signedTransport{config: cfg, token: token, payloadHash: fmt.Sprintf("%x", sha256.Sum256(body))},
		// Do not forward bearer tokens or sign redirected requests to other hosts.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

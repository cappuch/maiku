package tools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mikus/maiku/agent"
	"github.com/mikus/maiku/ai"
)

const (
	defaultCurlTimeout = 30 * time.Second
	maxCurlTimeout     = 120 * time.Second
)

var curlSchema = []byte(`{
	"type": "object",
	"properties": {
		"url": {"type": "string", "minLength": 1, "description": "HTTP or HTTPS URL to fetch"},
		"method": {"type": "string", "enum": ["GET", "HEAD", "POST", "PUT", "PATCH", "DELETE"], "description": "HTTP method (default: GET)"},
		"headers": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Optional request headers"},
		"body": {"type": "string", "description": "Optional request body"},
		"timeout": {"type": "number", "exclusiveMinimum": 0, "maximum": 120, "description": "Timeout in seconds (default: 30, maximum: 120)"}
	},
	"required": ["url"],
	"additionalProperties": false
}`)

// CurlToolDetails describes the HTTP response returned by the curl tool.
type CurlToolDetails struct {
	StatusCode  int    `json:"statusCode"`
	URL         string `json:"url"`
	ContentType string `json:"contentType,omitempty"`
	Truncated   bool   `json:"truncated,omitempty"`
}

// CreateCurlTool creates a browser-emulating HTTP content fetcher.
func CreateCurlTool() *agent.AgentTool {
	return CreateCurlToolWithClient(&http.Client{})
}

// CreateCurlToolWithClient creates a curl tool using client. It is exported
// for hosts that need a custom transport and for deterministic tests.
func CreateCurlToolWithClient(client *http.Client) *agent.AgentTool {
	return &agent.AgentTool{
		Tool: ai.Tool{
			Name: string(ToolCurl),
			Description: fmt.Sprintf(
				"Fetch an HTTP(S) URL and return the response body. Uses a Chrome desktop user agent and follows redirects. Output is truncated to the first %d lines or %dKB.",
				DefaultMaxLines, DefaultMaxBytes/1024,
			),
			Parameters: curlSchema,
		},
		Label: string(ToolCurl),
		Execute: func(ctx context.Context, _ string, params map[string]any, _ agent.AgentToolUpdateCallback) (agent.AgentToolResult, error) {
			if err := checkAborted(ctx); err != nil {
				return agent.AgentToolResult{}, err
			}

			rawURL := strings.TrimSpace(argStringOr(params, "url", ""))
			if err := validateHTTPURL(rawURL); err != nil {
				return agent.AgentToolResult{}, err
			}
			method := strings.ToUpper(argStringOr(params, "method", http.MethodGet))
			body := argStringOr(params, "body", "")
			req, err := http.NewRequestWithContext(ctx, method, rawURL, strings.NewReader(body))
			if err != nil {
				return agent.AgentToolResult{}, fmt.Errorf("build HTTP request: %w", err)
			}
			setBrowserHeaders(req.Header)
			if body != "" {
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			}
			switch headers := params["headers"].(type) {
			case map[string]any:
				for name, value := range headers {
					text, stringValue := value.(string)
					if !stringValue {
						return agent.AgentToolResult{}, fmt.Errorf("header %q must be a string", name)
					}
					req.Header.Set(name, text)
				}
			case map[string]string:
				for name, value := range headers {
					req.Header.Set(name, value)
				}
			}

			timeout := defaultCurlTimeout
			if seconds := argNumberPtr(params, "timeout"); seconds != nil {
				timeout = time.Duration(*seconds * float64(time.Second))
				if timeout <= 0 || timeout > maxCurlTimeout {
					return agent.AgentToolResult{}, fmt.Errorf("timeout must be greater than 0 and at most %g seconds", maxCurlTimeout.Seconds())
				}
			}
			runCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			req = req.WithContext(runCtx)

			httpClient := client
			if httpClient == nil {
				httpClient = &http.Client{}
			}
			resp, err := httpClient.Do(req)
			if err != nil {
				if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
					return agent.AgentToolResult{}, fmt.Errorf("HTTP request timed out after %g seconds", timeout.Seconds())
				}
				return agent.AgentToolResult{}, fmt.Errorf("HTTP request: %w", err)
			}
			defer resp.Body.Close()

			data, err := io.ReadAll(io.LimitReader(resp.Body, DefaultMaxBytes+1))
			if err != nil {
				if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
					return agent.AgentToolResult{}, fmt.Errorf("HTTP request timed out after %g seconds", timeout.Seconds())
				}
				return agent.AgentToolResult{}, fmt.Errorf("read HTTP response: %w", err)
			}
			content, truncated := truncateWebContent(string(data))
			if content == "" {
				content = "(empty body)"
			}

			finalURL := rawURL
			if resp.Request != nil && resp.Request.URL != nil {
				finalURL = resp.Request.URL.String()
			}
			contentType := resp.Header.Get("Content-Type")
			output := fmt.Sprintf("HTTP %s\nURL: %s", resp.Status, finalURL)
			if contentType != "" {
				output += "\nContent-Type: " + contentType
			}
			output += "\n\n" + content
			if truncated {
				output += fmt.Sprintf("\n\n[Response body truncated to the first %d lines or %s.]", DefaultMaxLines, FormatSize(DefaultMaxBytes))
			}

			return agent.AgentToolResult{
				Content: []ai.ToolResultContent{{Type: "text", Text: output}},
				Details: CurlToolDetails{
					StatusCode:  resp.StatusCode,
					URL:         finalURL,
					ContentType: contentType,
					Truncated:   truncated,
				},
			}, nil
		},
	}
}

func validateHTTPURL(raw string) error {
	if raw == "" {
		return errors.New("URL cannot be empty")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("URL must use http or https")
	}
	if parsed.Host == "" {
		return errors.New("URL must include a host")
	}
	return nil
}

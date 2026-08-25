package ai

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	defaultHTTPMaxRetries  = 3
	defaultHTTPBaseDelayMs = 500
	defaultHTTPMaxDelayMs  = 30000
)

// HTTPRetryPolicy controls retries for the initial provider HTTP request
// (before a streaming body is consumed).
type HTTPRetryPolicy struct {
	MaxRetries  int
	BaseDelayMs int
	MaxDelayMs  int
}

// HTTPRetryPolicyFromStreamOptions builds a policy from SimpleStreamOptions.
func HTTPRetryPolicyFromStreamOptions(opts *SimpleStreamOptions) HTTPRetryPolicy {
	policy := HTTPRetryPolicy{
		MaxRetries:  defaultHTTPMaxRetries,
		BaseDelayMs: defaultHTTPBaseDelayMs,
		MaxDelayMs:  defaultHTTPMaxDelayMs,
	}
	if opts == nil {
		return policy
	}
	if opts.MaxRetries != nil {
		policy.MaxRetries = *opts.MaxRetries
	}
	if opts.MaxRetryDelayMs != nil && *opts.MaxRetryDelayMs > 0 {
		policy.MaxDelayMs = *opts.MaxRetryDelayMs
	}
	return policy
}

// CapDelayMs clamps an exponential delay to maxDelayMs.
func CapDelayMs(delayMs, maxDelayMs int) int {
	if maxDelayMs > 0 && delayMs > maxDelayMs {
		return maxDelayMs
	}
	if delayMs < 0 {
		return 0
	}
	return delayMs
}

// ExponentialDelayMs returns baseDelay * 2^(attempt-1), capped.
func ExponentialDelayMs(baseDelayMs, attempt, maxDelayMs int) int {
	if baseDelayMs <= 0 {
		baseDelayMs = defaultHTTPBaseDelayMs
	}
	if attempt < 1 {
		attempt = 1
	}
	delay := baseDelayMs
	for i := 1; i < attempt; i++ {
		if maxDelayMs > 0 && delay >= maxDelayMs {
			return maxDelayMs
		}
		next := delay * 2
		if next < delay {
			return CapDelayMs(delay, maxDelayMs)
		}
		delay = next
	}
	return CapDelayMs(delay, maxDelayMs)
}

// IsRetryableHTTPStatus reports whether an HTTP status should be retried.
func IsRetryableHTTPStatus(status int) bool {
	switch status {
	case 408, 409, 425, 429, 500, 502, 503, 504, 529:
		return true
	default:
		return false
	}
}

// IsRetryableHTTPError reports whether a transport error should be retried.
func IsRetryableHTTPError(err error) bool {
	if err == nil {
		return false
	}
	if err == context.Canceled {
		return false
	}
	msg := AssistantMessage{StopReason: StopError, ErrorMessage: err.Error()}
	return IsRetryableAssistantError(msg)
}

// DoHTTP executes req with exponential backoff on transient failures.
//
// The request body is buffered so retries can resend it. On non-retryable
// HTTP error statuses the response is returned for the caller to read.
func DoHTTP(client *http.Client, req *http.Request, policy HTTPRetryPolicy) (*http.Response, error) {
	if client == nil {
		client = http.DefaultClient
	}
	if policy.MaxRetries < 0 {
		policy.MaxRetries = 0
	}
	if policy.BaseDelayMs <= 0 {
		policy.BaseDelayMs = defaultHTTPBaseDelayMs
	}
	if policy.MaxDelayMs <= 0 {
		policy.MaxDelayMs = defaultHTTPMaxDelayMs
	}

	var body []byte
	if req.Body != nil {
		var err error
		body, err = io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			return nil, err
		}
	}

	var lastErr error
	for attempt := 0; attempt <= policy.MaxRetries; attempt++ {
		if attempt > 0 {
			delay := ExponentialDelayMs(policy.BaseDelayMs, attempt, policy.MaxDelayMs)
			timer := time.NewTimer(time.Duration(delay) * time.Millisecond)
			select {
			case <-req.Context().Done():
				timer.Stop()
				if lastErr != nil {
					return nil, lastErr
				}
				return nil, req.Context().Err()
			case <-timer.C:
			}
		}

		retryReq := req.Clone(req.Context())
		if body != nil {
			retryReq.Body = io.NopCloser(bytes.NewReader(body))
			retryReq.ContentLength = int64(len(body))
			retryReq.GetBody = func() (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(body)), nil
			}
		}

		resp, err := client.Do(retryReq)
		if err != nil {
			lastErr = err
			if attempt == policy.MaxRetries || !IsRetryableHTTPError(err) {
				return nil, err
			}
			continue
		}
		if IsRetryableHTTPStatus(resp.StatusCode) && attempt < policy.MaxRetries {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("provider API error (%d)", resp.StatusCode)
			continue
		}
		return resp, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("http request failed after retries")
}

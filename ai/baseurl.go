package ai

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// BaseURLWithV1 appends /v1 when base does not already end in that segment.
// https://example.com becomes https://example.com/v1. A base that already ends
// in /v1 is left alone.
func BaseURLWithV1(base string) (string, bool) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return "", false
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", false
	}
	path := strings.TrimRight(parsed.Path, "/")
	if strings.HasSuffix(strings.ToLower(path), "/v1") {
		return "", false
	}
	parsed.Path = path + "/v1"
	return strings.TrimRight(parsed.String(), "/"), true
}

var (
	v1Mu    sync.Mutex
	v1Bases = map[string]string{}
	v1Allow func(providerID, baseURL string) bool
	v1Saved func(providerID, baseURL string)
)

// SetV1FallbackPolicy decides which providers may grow a /v1 prefix after a
// request 404s. A nil policy disables the fallback.
func SetV1FallbackPolicy(allow func(providerID, baseURL string) bool) {
	v1Mu.Lock()
	v1Allow = allow
	v1Mu.Unlock()
}

// SetBaseURLCorrectedHook is called after a /v1 retry succeeds so the host
// can store the URL that actually worked.
func SetBaseURLCorrectedHook(saved func(providerID, baseURL string)) {
	v1Mu.Lock()
	v1Saved = saved
	v1Mu.Unlock()
}

// UseCorrectedBaseURL returns a base URL learned from an earlier /v1 retry.
func UseCorrectedBaseURL(providerID string) string {
	v1Mu.Lock()
	defer v1Mu.Unlock()
	return v1Bases[providerID]
}

// ForgetCorrectedBaseURL drops a learned base URL so a later edit wins.
func ForgetCorrectedBaseURL(providerID string) {
	v1Mu.Lock()
	delete(v1Bases, providerID)
	v1Mu.Unlock()
}

// AllowV1Fallback reports whether a 404 for this base should be retried with /v1.
func AllowV1Fallback(providerID, baseURL string) bool {
	if _, ok := BaseURLWithV1(baseURL); !ok {
		return false
	}
	v1Mu.Lock()
	allow := v1Allow
	v1Mu.Unlock()
	if allow == nil {
		return false
	}
	return allow(providerID, baseURL)
}

// NoteCorrectedBaseURL remembers a base URL that worked only after /v1 was added.
func NoteCorrectedBaseURL(providerID, baseURL string) {
	providerID = strings.TrimSpace(providerID)
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if providerID == "" || baseURL == "" {
		return
	}
	v1Mu.Lock()
	v1Bases[providerID] = baseURL
	saved := v1Saved
	v1Mu.Unlock()
	if saved != nil {
		saved(providerID, baseURL)
	}
}

// PromoteOpenAIV1 reissues a 404 against baseURL+"/v1"+suffix when this
// provider is allowed to. The caller must close the returned body. On a
// failed retry the original 404 response is returned with its body restored.
func PromoteOpenAIV1(client *http.Client, resp *http.Response, policy HTTPRetryPolicy, providerID, baseURL, suffix string, body []byte) (*http.Response, string) {
	if resp == nil || resp.StatusCode != http.StatusNotFound || resp.Request == nil {
		return resp, baseURL
	}
	if !AllowV1Fallback(providerID, baseURL) {
		return resp, baseURL
	}
	fixed, ok := BaseURLWithV1(baseURL)
	if !ok {
		return resp, baseURL
	}
	errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()

	ctx := resp.Request.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	retry, err := http.NewRequestWithContext(ctx, resp.Request.Method, fixed+suffix, bytes.NewReader(body))
	if err != nil {
		resp.Body = io.NopCloser(bytes.NewReader(errBody))
		return resp, baseURL
	}
	retry.Header = resp.Request.Header.Clone()
	next, err := DoHTTP(client, retry, policy)
	if err != nil || next == nil || next.StatusCode >= 400 {
		if next != nil {
			_ = next.Body.Close()
		}
		resp.Body = io.NopCloser(bytes.NewReader(errBody))
		resp.StatusCode = http.StatusNotFound
		return resp, baseURL
	}
	NoteCorrectedBaseURL(providerID, fixed)
	return next, fixed
}

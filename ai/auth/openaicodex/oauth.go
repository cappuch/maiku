// Package openaicodex implements ChatGPT / Codex OAuth (device-code + refresh).
package openaicodex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	clientID              = "app_EMoamEEZ73f0CkXaXp7hrann"
	authBaseURL           = "https://auth.openai.com"
	tokenURL              = authBaseURL + "/oauth/token"
	deviceUserCodeURL     = authBaseURL + "/api/accounts/deviceauth/usercode"
	deviceTokenURL        = authBaseURL + "/api/accounts/deviceauth/token"
	deviceVerificationURI = authBaseURL + "/codex/device"
	deviceRedirectURI     = authBaseURL + "/deviceauth/callback"
	deviceCodeTimeout     = 15 * time.Minute
)

// flexibleSeconds unmarshals either a JSON number (seconds) or a JSON string
// containing a number (seconds). OpenAI's device-auth endpoint has returned
// interval as both shapes over time.
type flexibleSeconds float64

func (s *flexibleSeconds) UnmarshalJSON(data []byte) error {
	if v, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64); err == nil {
		*s = flexibleSeconds(v)
		return nil
	}
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		v, err := strconv.ParseFloat(strings.TrimSpace(str), 64)
		if err != nil {
			return err
		}
		*s = flexibleSeconds(v)
		return nil
	}
	return fmt.Errorf("cannot unmarshal interval value %s as seconds", string(data))
}

// Token is a refreshed / exchanged OAuth token set.
type Token struct {
	Access  string
	Refresh string
	Expires time.Time // absolute expiry
}

// DeviceAuth is the pending device-code challenge shown to the user.
type DeviceAuth struct {
	DeviceAuthID    string
	UserCode        string
	Interval        time.Duration
	VerificationURI string
}

// StartDeviceAuth begins the headless Codex login flow.
func StartDeviceAuth(ctx context.Context) (*DeviceAuth, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, deviceUserCodeURL, strings.NewReader(`{"client_id":"`+clientID+`"}`))
	if err != nil {
		return nil, err
	}
	req.Header.Set("content-type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	closeErr := resp.Body.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("openai-codex device code login is not enabled for this server")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai-codex device code request failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var parsed struct {
		DeviceAuthID string          `json:"device_auth_id"`
		UserCode     string          `json:"user_code"`
		Interval     flexibleSeconds `json:"interval"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	if parsed.DeviceAuthID == "" || parsed.UserCode == "" {
		return nil, fmt.Errorf("invalid openai-codex device code response: %s", string(body))
	}
	interval := time.Duration(float64(parsed.Interval) * float64(time.Second))
	if interval < time.Second {
		interval = 5 * time.Second
	}
	return &DeviceAuth{
		DeviceAuthID:    parsed.DeviceAuthID,
		UserCode:        parsed.UserCode,
		Interval:        interval,
		VerificationURI: deviceVerificationURI,
	}, nil
}

// PollDeviceAuth waits until the user completes device login, then returns tokens.
func PollDeviceAuth(ctx context.Context, device *DeviceAuth) (*Token, error) {
	if device == nil {
		return nil, fmt.Errorf("device auth is required")
	}
	deadline := time.Now().Add(deviceCodeTimeout)
	interval := device.Interval
	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("openai-codex device login timed out")
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		payload, _ := json.Marshal(map[string]string{
			"device_auth_id": device.DeviceAuthID,
			"user_code":      device.UserCode,
		})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, deviceTokenURL, strings.NewReader(string(payload)))
		if err != nil {
			return nil, err
		}
		req.Header.Set("content-type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		closeErr := resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}

		if resp.StatusCode == http.StatusOK {
			var parsed struct {
				AuthorizationCode string `json:"authorization_code"`
				CodeVerifier      string `json:"code_verifier"`
			}
			if err := json.Unmarshal(body, &parsed); err != nil {
				return nil, err
			}
			if parsed.AuthorizationCode == "" || parsed.CodeVerifier == "" {
				return nil, fmt.Errorf("invalid openai-codex device auth token response: %s", string(body))
			}
			return ExchangeAuthorizationCode(ctx, parsed.AuthorizationCode, parsed.CodeVerifier, deviceRedirectURI)
		}

		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(interval):
			}
			continue
		}

		var errJSON struct {
			Error any `json:"error"`
		}
		_ = json.Unmarshal(body, &errJSON)
		code := ""
		switch v := errJSON.Error.(type) {
		case string:
			code = v
		case map[string]any:
			if s, ok := v["code"].(string); ok {
				code = s
			}
		}
		switch code {
		case "deviceauth_authorization_pending":
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(interval):
			}
			continue
		case "slow_down":
			interval += 5 * time.Second
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(interval):
			}
			continue
		default:
			return nil, fmt.Errorf("openai-codex device auth failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
	}
}

// ExchangeAuthorizationCode swaps an auth code + PKCE verifier for tokens.
func ExchangeAuthorizationCode(ctx context.Context, code, verifier, redirectURI string) (*Token, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {redirectURI},
	}
	return postToken(ctx, form, "exchange")
}

// Refresh exchanges a refresh token for a new access token.
func Refresh(ctx context.Context, refreshToken string) (*Token, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {clientID},
	}
	return postToken(ctx, form, "refresh")
}

func postToken(ctx context.Context, form url.Values, operation string) (*Token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("content-type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai-codex token %s error: %w", operation, err)
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	closeErr := resp.Body.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai-codex token %s failed (%d): %s", operation, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var parsed struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	if parsed.AccessToken == "" || parsed.RefreshToken == "" || parsed.ExpiresIn == 0 {
		return nil, fmt.Errorf("openai-codex token %s response missing fields: %s", operation, string(body))
	}
	return &Token{
		Access:  parsed.AccessToken,
		Refresh: parsed.RefreshToken,
		Expires: time.Now().Add(time.Duration(parsed.ExpiresIn) * time.Second),
	}, nil
}

package ai

import (
	"context"
	"regexp"
	"time"
)

var (
	nonRetryableProviderLimitError = regexp.MustCompile(`(?i)GoUsageLimitError|FreeUsageLimitError|Monthly usage limit reached|available balance|insufficient_quota|out of budget|quota exceeded|billing`)
	retryableProviderError         = regexp.MustCompile(`(?i)overloaded|rate.?limit|too many requests|429|500|502|503|504|524|service.?unavailable|server.?error|internal.?error|provider.?returned.?error|exceeded request buffer limit while retrying upstream|network.?error|connection.?error|connection.?refused|connection.?lost|other side closed|fetch failed|getaddrinfo|ENOTFOUND|EAI_AGAIN|upstream.?connect|reset before headers|socket hang up|socket connection was closed|tls:? bad record mac|timed? out|timeout|terminated|websocket.?closed|websocket.?error|ended without|stream ended before message_stop|stream ended before a terminal response event|http2 request did not get a response|retry delay|you can retry your request|try your request again|please retry your request|ResourceExhausted`)
)

// RetryPolicy mirrors settings.retry in the coding agent.
type RetryPolicy struct {
	Enabled     bool
	MaxRetries  int
	BaseDelayMs int
	// MaxDelayMs caps exponential backoff. Zero uses DefaultMaxRetryDelayMs.
	MaxDelayMs int
}

// IsRetryableAssistantError classifies transient provider/transport errors.
func IsRetryableAssistantError(message AssistantMessage) bool {
	if message.StopReason != StopError || message.ErrorMessage == "" {
		return false
	}
	if nonRetryableProviderLimitError.MatchString(message.ErrorMessage) {
		return false
	}
	return retryableProviderError.MatchString(message.ErrorMessage)
}

// RetryCallbacks are optional hooks around each retry attempt.
type RetryCallbacks struct {
	OnRetryScheduled    func(attempt, maxAttempts, delayMs int, errorMessage string)
	OnRetryAttemptStart func()
	OnRetryFinished     func(success bool, attempt int, finalError string)
}

// RetryAssistantCall runs produce with bounded exponential backoff on
// retryable errors. Aborts are never retried.
func RetryAssistantCall(
	ctx context.Context,
	produce func() (AssistantMessage, error),
	policy *RetryPolicy,
	callbacks *RetryCallbacks,
) (AssistantMessage, error) {
	maxAttempts := 0
	if policy != nil && policy.Enabled {
		maxAttempts = policy.MaxRetries
	}
	baseDelay := 2000
	if policy != nil && policy.BaseDelayMs > 0 {
		baseDelay = policy.BaseDelayMs
	}

	attempt := 0
	var lastRetryAttempt int
	var lastRetryError string
	hadRetry := false

	for {
		response, err := produce()
		if err != nil {
			return AssistantMessage{}, err
		}

		if response.StopReason == StopAborted {
			if hadRetry && callbacks != nil && callbacks.OnRetryFinished != nil {
				callbacks.OnRetryFinished(false, lastRetryAttempt, "")
			}
			return response, nil
		}
		if response.StopReason != StopError {
			if hadRetry && callbacks != nil && callbacks.OnRetryFinished != nil {
				callbacks.OnRetryFinished(true, lastRetryAttempt, "")
			}
			return response, nil
		}
		if attempt >= maxAttempts || !IsRetryableAssistantError(response) {
			if hadRetry && callbacks != nil && callbacks.OnRetryFinished != nil {
				callbacks.OnRetryFinished(false, lastRetryAttempt, response.ErrorMessage)
			}
			return response, nil
		}

		attempt++
		hadRetry = true
		lastRetryAttempt = attempt
		lastRetryError = response.ErrorMessage
		if lastRetryError == "" {
			lastRetryError = "Unknown error"
		}
		delayMs := baseDelay * (1 << (attempt - 1))
		maxDelay := 60000
		if policy != nil && policy.MaxDelayMs > 0 {
			maxDelay = policy.MaxDelayMs
		}
		delayMs = CapDelayMs(delayMs, maxDelay)
		if callbacks != nil && callbacks.OnRetryScheduled != nil {
			callbacks.OnRetryScheduled(attempt, maxAttempts, delayMs, lastRetryError)
		}

		timer := time.NewTimer(time.Duration(delayMs) * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			if callbacks != nil && callbacks.OnRetryFinished != nil {
				callbacks.OnRetryFinished(false, attempt, lastRetryError)
			}
			aborted := response
			aborted.StopReason = StopAborted
			aborted.ErrorMessage = ""
			return aborted, nil
		case <-timer.C:
		}
		if callbacks != nil && callbacks.OnRetryAttemptStart != nil {
			callbacks.OnRetryAttemptStart()
		}
	}
}

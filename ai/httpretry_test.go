package ai

import "testing"

func TestExponentialDelayMs(t *testing.T) {
	if got := ExponentialDelayMs(500, 1, 30000); got != 500 {
		t.Fatalf("attempt1=%d", got)
	}
	if got := ExponentialDelayMs(500, 2, 30000); got != 1000 {
		t.Fatalf("attempt2=%d", got)
	}
	if got := ExponentialDelayMs(500, 10, 2000); got != 2000 {
		t.Fatalf("capped=%d", got)
	}
}

func TestIsRetryableHTTPStatus(t *testing.T) {
	if !IsRetryableHTTPStatus(429) || !IsRetryableHTTPStatus(503) {
		t.Fatal("expected 429/503 retryable")
	}
	if IsRetryableHTTPStatus(400) || IsRetryableHTTPStatus(401) {
		t.Fatal("expected 400/401 non-retryable")
	}
}

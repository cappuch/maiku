package tools

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCurlToolFetchesWithBrowserUserAgent(t *testing.T) {
	var requestError string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			requestError = "method = " + r.Method
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if got := r.Header.Get("User-Agent"); got != BrowserUserAgent {
			requestError = "user-agent = " + got
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if got := r.Header.Get("X-Test"); got != "yes" {
			requestError = "X-Test = " + got
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("<html><body>received</body></html>"))
	}))
	defer server.Close()

	tool := CreateCurlToolWithClient(server.Client())
	result, err := tool.Execute(context.Background(), "call-1", map[string]any{
		"url":     server.URL,
		"method":  "POST",
		"headers": map[string]any{"X-Test": "yes"},
		"body":    "q=hello",
	}, nil)
	if err != nil {
		t.Fatalf("curl Execute returned error: %v (request error: %s)", err, requestError)
	}
	if requestError != "" {
		t.Fatal(requestError)
	}
	if len(result.Content) != 1 {
		t.Fatalf("tool content length = %d", len(result.Content))
	}
	output := result.Content[0].Text
	for _, expected := range []string{
		"HTTP 201 Created",
		"Content-Type: text/html; charset=utf-8",
		"<html><body>received</body></html>",
	} {
		if !strings.Contains(output, expected) {
			t.Errorf("output does not contain %q:\n%s", expected, output)
		}
	}
	details, ok := result.Details.(CurlToolDetails)
	if !ok || details.StatusCode != http.StatusCreated || details.Truncated {
		t.Fatalf("details = %#v", result.Details)
	}
}

func TestCurlToolRejectsNonHTTPURL(t *testing.T) {
	tool := CreateCurlToolWithClient(nil)
	_, err := tool.Execute(context.Background(), "call-1", map[string]any{"url": "file:///etc/passwd"}, nil)
	if err == nil || !strings.Contains(err.Error(), "http or https") {
		t.Fatalf("non-HTTP URL error = %v", err)
	}
}

func TestCurlToolTruncatesMinifiedResponses(t *testing.T) {
	body := strings.Repeat("x", DefaultMaxBytes+100)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	tool := CreateCurlToolWithClient(server.Client())
	result, err := tool.Execute(context.Background(), "call-1", map[string]any{"url": server.URL}, nil)
	if err != nil {
		t.Fatalf("curl Execute returned error: %v", err)
	}
	output := result.Content[0].Text
	if !strings.Contains(output, "Response body truncated") {
		t.Fatalf("missing truncation notice: %s", output[len(output)-200:])
	}
	if !strings.Contains(output, strings.Repeat("x", 100)) {
		t.Fatal("minified body was omitted")
	}
	details := result.Details.(CurlToolDetails)
	if !details.Truncated {
		t.Fatalf("details did not report truncation: %+v", details)
	}
}

func TestCurlToolHonorsTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		_, _ = fmt.Fprint(w, "late")
	}))
	defer server.Close()

	tool := CreateCurlToolWithClient(server.Client())
	_, err := tool.Execute(context.Background(), "call-1", map[string]any{
		"url": server.URL, "timeout": 0.01,
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("timeout error = %v", err)
	}
}

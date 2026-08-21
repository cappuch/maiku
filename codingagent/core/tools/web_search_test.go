package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const duckDuckGoHTMLFixture = `<!doctype html>
<html><body><div id="links">
  <div class="result results_links results_links_deep web-result">
    <h2 class="result__title">
      <a class="result__a" href="https://example.com/docs">Example &amp; Documentation</a>
    </h2>
    <a class="result__snippet" href="https://example.com/docs">A <b>useful</b> result &amp; reference.</a>
  </div>
  <div class="result results_links web-result">
    <h2><a class="result__a" href="/l/?uddg=https%3A%2F%2Fgo.dev%2Fdoc%2F&amp;rut=abc">The Go Programming Language</a></h2>
    <a class="result__snippet">Build simple, secure, scalable systems.</a>
  </div>
  <div class="result web-result">
    <h2><a class="result__a" href="https://example.com/docs">Duplicate</a></h2>
  </div>
</div></body></html>`

func TestParseDuckDuckGoHTML(t *testing.T) {
	results, err := ParseDuckDuckGoHTML(strings.NewReader(duckDuckGoHTMLFixture), 10)
	if err != nil {
		t.Fatalf("ParseDuckDuckGoHTML returned error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2: %+v", len(results), results)
	}
	if got, want := results[0].Title, "Example & Documentation"; got != want {
		t.Errorf("first title = %q, want %q", got, want)
	}
	if got, want := results[0].Snippet, "A useful result & reference."; got != want {
		t.Errorf("first snippet = %q, want %q", got, want)
	}
	if got, want := results[1].URL, "https://go.dev/doc/"; got != want {
		t.Errorf("redirect URL = %q, want %q", got, want)
	}

	limited, err := ParseDuckDuckGoHTML(strings.NewReader(duckDuckGoHTMLFixture), 1)
	if err != nil {
		t.Fatalf("limited parse returned error: %v", err)
	}
	if len(limited) != 1 {
		t.Fatalf("limited parse returned %d results, want 1", len(limited))
	}
}

func TestDuckDuckGoSearchServiceRequestAndResponse(t *testing.T) {
	var requestError string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			requestError = "method = " + r.Method
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if err := r.ParseForm(); err != nil {
			requestError = "parse form: " + err.Error()
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		checks := map[string]string{
			"q":  "Go HTML parser",
			"b":  "",
			"kl": "uk-en",
			"df": "w",
		}
		for key, want := range checks {
			if got := r.Form.Get(key); got != want {
				requestError = key + " = " + got
				w.WriteHeader(http.StatusBadRequest)
				return
			}
		}
		if got := r.Header.Get("User-Agent"); got != BrowserUserAgent {
			requestError = "user-agent = " + got
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if got := r.Header.Get("Accept"); got != browserAccept {
			requestError = "accept = " + got
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if got := r.Header.Get("Origin"); got != "https://html.duckduckgo.com" {
			requestError = "origin = " + got
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		_, _ = w.Write([]byte(duckDuckGoHTMLFixture))
	}))
	defer server.Close()

	service := &DuckDuckGoSearchService{Client: server.Client(), Endpoint: server.URL}
	response, err := service.Search(context.Background(), WebSearchRequest{
		Query: "  Go HTML parser  ", MaxResults: 1, Region: "uk-en", Time: "w",
	})
	if err != nil {
		t.Fatalf("Search returned error: %v (request error: %s)", err, requestError)
	}
	if requestError != "" {
		t.Fatal(requestError)
	}
	if response.Query != "Go HTML parser" {
		t.Errorf("query = %q", response.Query)
	}
	if len(response.Results) != 1 || response.Results[0].URL != "https://example.com/docs" {
		t.Fatalf("unexpected results: %+v", response.Results)
	}
}

type stubWebSearchService struct {
	request WebSearchRequest
}

func (s *stubWebSearchService) Search(_ context.Context, request WebSearchRequest) (WebSearchResponse, error) {
	s.request = request
	return WebSearchResponse{
		Query:   request.Query,
		Results: []WebSearchResult{{Title: "Result", URL: "https://example.com", Snippet: "Snippet"}},
	}, nil
}

func TestWebSearchToolReturnsJSON(t *testing.T) {
	service := &stubWebSearchService{}
	tool := CreateWebSearchToolWithService(service)
	result, err := tool.Execute(context.Background(), "call-1", map[string]any{
		"query": "test query", "max_results": float64(3), "region": "us-en", "time": "d",
	}, nil)
	if err != nil {
		t.Fatalf("web_search Execute returned error: %v", err)
	}
	if service.request.MaxResults != 3 || service.request.Region != "us-en" || service.request.Time != "d" {
		t.Fatalf("service request = %+v", service.request)
	}
	if len(result.Content) != 1 {
		t.Fatalf("tool content length = %d", len(result.Content))
	}
	var response WebSearchResponse
	if err := json.Unmarshal([]byte(result.Content[0].Text), &response); err != nil {
		t.Fatalf("tool output is not JSON: %v\n%s", err, result.Content[0].Text)
	}
	if len(response.Results) != 1 || response.Results[0].Title != "Result" {
		t.Fatalf("tool response = %+v", response)
	}
}

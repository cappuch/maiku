package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mikus/maiku/agent"
	"github.com/mikus/maiku/ai"
	"golang.org/x/net/html"
)

const (
	duckDuckGoHTMLEndpoint = "https://html.duckduckgo.com/html/"
	defaultSearchResults   = 10
	maxSearchResults       = 20
	maxSearchResponseBytes = 2 * 1024 * 1024
)

var webSearchSchema = []byte(`{
	"type": "object",
	"properties": {
		"query": {"type": "string", "minLength": 1, "description": "Search query"},
		"max_results": {"type": "integer", "minimum": 1, "maximum": 20, "description": "Maximum number of results (default: 10)"},
		"region": {"type": "string", "description": "Optional DuckDuckGo region code, for example us-en or uk-en"},
		"time": {"type": "string", "enum": ["d", "w", "m", "y"], "description": "Optional time range: d=day, w=week, m=month, y=year"}
	},
	"required": ["query"],
	"additionalProperties": false
}`)

// WebSearchRequest is the in-process API used by the web_search tool.
type WebSearchRequest struct {
	Query      string `json:"query"`
	MaxResults int    `json:"max_results,omitempty"`
	Region     string `json:"region,omitempty"`
	Time       string `json:"time,omitempty"`
}

// WebSearchResult is one organic result scraped from DuckDuckGo HTML.
type WebSearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet,omitempty"`
}

// WebSearchResponse is returned by a WebSearchService and encoded for the LLM.
type WebSearchResponse struct {
	Query   string            `json:"query"`
	Results []WebSearchResult `json:"results"`
}

// WebSearchService allows the scraper API to be replaced in tests or by hosts.
type WebSearchService interface {
	Search(context.Context, WebSearchRequest) (WebSearchResponse, error)
}

// DuckDuckGoSearchService scrapes DuckDuckGo's no-JavaScript HTML endpoint.
type DuckDuckGoSearchService struct {
	Client   *http.Client
	Endpoint string
}

// NewDuckDuckGoSearchService creates a browser-emulating DDG HTML client.
func NewDuckDuckGoSearchService() *DuckDuckGoSearchService {
	return &DuckDuckGoSearchService{
		Client:   &http.Client{Timeout: 30 * time.Second},
		Endpoint: duckDuckGoHTMLEndpoint,
	}
}

// Search posts a query to DuckDuckGo HTML and returns parsed organic results.
func (s *DuckDuckGoSearchService) Search(ctx context.Context, request WebSearchRequest) (WebSearchResponse, error) {
	query := strings.TrimSpace(request.Query)
	if query == "" {
		return WebSearchResponse{}, errors.New("search query cannot be empty")
	}
	limit := request.MaxResults
	if limit <= 0 {
		limit = defaultSearchResults
	}
	limit = min(limit, maxSearchResults)

	endpoint := s.Endpoint
	if endpoint == "" {
		endpoint = duckDuckGoHTMLEndpoint
	}
	form := url.Values{"q": {query}, "b": {""}}
	if request.Region != "" {
		form.Set("kl", request.Region)
	}
	if request.Time != "" {
		form.Set("df", request.Time)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return WebSearchResponse{}, fmt.Errorf("build DuckDuckGo request: %w", err)
	}
	setBrowserHeaders(req.Header)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://html.duckduckgo.com")
	req.Header.Set("Referer", "https://html.duckduckgo.com/")
	req.Header.Set("Priority", "u=0, i")
	req.Header.Set("Sec-CH-UA", `"Not=A?Brand";v="99", "Google Chrome";v="151", "Chromium";v="151"`)
	req.Header.Set("Sec-CH-UA-Mobile", "?0")
	req.Header.Set("Sec-CH-UA-Platform", `"macOS"`)
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-User", "?1")

	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return WebSearchResponse{}, fmt.Errorf("DuckDuckGo request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSearchResponseBytes+1))
	if err != nil {
		return WebSearchResponse{}, fmt.Errorf("read DuckDuckGo response: %w", err)
	}
	if len(body) > maxSearchResponseBytes {
		return WebSearchResponse{}, fmt.Errorf("DuckDuckGo response exceeds %s", FormatSize(maxSearchResponseBytes))
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		message, _ := TruncateLine(strings.TrimSpace(string(body)), 1000)
		if message != "" {
			return WebSearchResponse{}, fmt.Errorf("DuckDuckGo returned %s: %s", resp.Status, message)
		}
		return WebSearchResponse{}, fmt.Errorf("DuckDuckGo returned %s", resp.Status)
	}

	results, err := ParseDuckDuckGoHTML(strings.NewReader(string(body)), limit)
	if err != nil {
		return WebSearchResponse{}, fmt.Errorf("parse DuckDuckGo response: %w", err)
	}
	return WebSearchResponse{Query: query, Results: results}, nil
}

// ParseDuckDuckGoHTML extracts organic search results from DDG's HTML page.
func ParseDuckDuckGoHTML(reader io.Reader, limit int) ([]WebSearchResult, error) {
	document, err := html.Parse(reader)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = defaultSearchResults
	}
	limit = min(limit, maxSearchResults)

	results := make([]WebSearchResult, 0, min(limit, defaultSearchResults))
	seen := make(map[string]struct{})
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if len(results) >= limit {
			return
		}
		if node.Type == html.ElementNode && hasHTMLClass(node, "result") && hasHTMLClass(node, "web-result") {
			if result, ok := parseDuckDuckGoResult(node); ok {
				if _, duplicate := seen[result.URL]; !duplicate {
					seen[result.URL] = struct{}{}
					results = append(results, result)
				}
			}
		}
		for child := node.FirstChild; child != nil && len(results) < limit; child = child.NextSibling {
			walk(child)
		}
	}
	walk(document)
	return results, nil
}

func parseDuckDuckGoResult(node *html.Node) (WebSearchResult, bool) {
	titleNode := findHTMLClass(node, "result__a")
	if titleNode == nil {
		return WebSearchResult{}, false
	}
	title := normalizedNodeText(titleNode)
	href := htmlAttribute(titleNode, "href")
	if title == "" || href == "" {
		return WebSearchResult{}, false
	}

	resultURL := normalizeDuckDuckGoResultURL(href)
	if resultURL == "" {
		return WebSearchResult{}, false
	}
	snippet := ""
	if snippetNode := findHTMLClass(node, "result__snippet"); snippetNode != nil {
		snippet = normalizedNodeText(snippetNode)
	}
	return WebSearchResult{Title: title, URL: resultURL, Snippet: snippet}, true
}

func hasHTMLClass(node *html.Node, class string) bool {
	for _, candidate := range strings.Fields(htmlAttribute(node, "class")) {
		if candidate == class {
			return true
		}
	}
	return false
}

func findHTMLClass(node *html.Node, class string) *html.Node {
	if node.Type == html.ElementNode && hasHTMLClass(node, class) {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findHTMLClass(child, class); found != nil {
			return found
		}
	}
	return nil
}

func htmlAttribute(node *html.Node, name string) string {
	for _, attribute := range node.Attr {
		if attribute.Key == name {
			return attribute.Val
		}
	}
	return ""
}

func normalizedNodeText(node *html.Node) string {
	var text strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current.Type == html.TextNode {
			text.WriteString(current.Data)
			text.WriteByte(' ')
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return strings.Join(strings.Fields(text.String()), " ")
}

func normalizeDuckDuckGoResultURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	isDDGHost := parsed.Hostname() == "" || parsed.Hostname() == "duckduckgo.com" || parsed.Hostname() == "html.duckduckgo.com"
	if isDDGHost && parsed.Path == "/l/" {
		if target := parsed.Query().Get("uddg"); target != "" {
			return target
		}
	}
	return raw
}

// CreateWebSearchTool exposes the DDG scraper to the LLM.
func CreateWebSearchTool() *agent.AgentTool {
	return CreateWebSearchToolWithService(NewDuckDuckGoSearchService())
}

// CreateWebSearchToolWithService builds a web_search tool around a service.
func CreateWebSearchToolWithService(service WebSearchService) *agent.AgentTool {
	return &agent.AgentTool{
		Tool: ai.Tool{
			Name:        string(ToolWebSearch),
			Description: "Search the web using DuckDuckGo's HTML endpoint. Returns organic result titles, URLs, and snippets. No API key is required.",
			Parameters:  webSearchSchema,
		},
		Label: string(ToolWebSearch),
		Execute: func(ctx context.Context, _ string, params map[string]any, _ agent.AgentToolUpdateCallback) (agent.AgentToolResult, error) {
			if err := checkAborted(ctx); err != nil {
				return agent.AgentToolResult{}, err
			}
			maxResults := defaultSearchResults
			if value := argIntPtr(params, "max_results"); value != nil {
				maxResults = *value
			}
			response, err := service.Search(ctx, WebSearchRequest{
				Query:      argStringOr(params, "query", ""),
				MaxResults: maxResults,
				Region:     argStringOr(params, "region", ""),
				Time:       argStringOr(params, "time", ""),
			})
			if err != nil {
				return agent.AgentToolResult{}, fmt.Errorf("web search: %w", err)
			}
			encoded, err := json.MarshalIndent(response, "", "  ")
			if err != nil {
				return agent.AgentToolResult{}, fmt.Errorf("encode web search results: %w", err)
			}
			return agent.AgentToolResult{Content: []ai.ToolResultContent{{Type: "text", Text: string(encoded)}}}, nil
		},
	}
}

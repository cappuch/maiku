package bedrock

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/cappuch/maiku/ai"
)

type foundationModel struct {
	ID        string   `json:"modelId"`
	Name      string   `json:"modelName"`
	Input     []string `json:"inputModalities"`
	Output    []string `json:"outputModalities"`
	Streaming bool     `json:"responseStreamingSupported"`
	Inference []string `json:"inferenceTypesSupported"`
}

// FetchModels discovers streaming text models and active inference profiles.
// baseURL, when provided, overrides the control-plane endpoint (useful for proxies).
func FetchModels(ctx context.Context, baseURL, apiKey string) ([]ai.Model, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cfg, err := loadConfig(ctx, nil)
	if err != nil {
		return nil, err
	}
	base := strings.TrimRight(baseURL, "/")
	if base == "" {
		base = endpoint(cfg, "bedrock")
	}
	httpClient := client(cfg, apiKey, nil)
	get := func(path string, target any) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
		if err != nil {
			return err
		}
		resp, err := ai.DoHTTP(httpClient, req, ai.HTTPRetryPolicyFromStreamOptions(nil))
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			return fmt.Errorf("Bedrock models API error (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(target)
	}
	var catalog struct {
		Models []foundationModel `json:"modelSummaries"`
	}
	if err := get("/foundation-models?byOutputModality=TEXT", &catalog); err != nil {
		return nil, err
	}
	byID := map[string]ai.Model{}
	models := []ai.Model{}
	for _, f := range catalog.Models {
		if !f.Streaming || !slices.Contains(f.Output, "TEXT") || f.ID == "" {
			continue
		}
		input := []string{"text"}
		if slices.Contains(f.Input, "IMAGE") {
			input = append(input, "image")
		}
		// The catalog does not expose token limits or pricing. Use conservative
		// defaults; callers can override max output tokens through stream options.
		m := ai.Model{ID: f.ID, Name: f.Name, Provider: ProviderID, API: ai.APIBedrockConverseStream, Input: input, MaxTokens: 4096, ContextWindow: 32000}
		m.Reasoning = adaptiveThinking(f.ID) || strings.Contains(f.ID, "anthropic.claude-sonnet-4") || strings.Contains(f.ID, "anthropic.claude-sonnet-5") || strings.Contains(f.ID, "anthropic.claude-opus-4") || strings.Contains(f.ID, "anthropic.claude-opus-5") || strings.Contains(f.ID, "anthropic.claude-fable") || strings.Contains(f.ID, "anthropic.claude-mythos") || strings.Contains(f.ID, "anthropic.claude-3-7")
		byID[f.ID] = m
		if slices.Contains(f.Inference, "ON_DEMAND") {
			models = append(models, m)
		}
	}
	seenTokens := map[string]bool{}
	path := "/inference-profiles?maxResults=100"
	for {
		var page struct {
			Profiles []struct {
				ID     string `json:"inferenceProfileId"`
				Name   string `json:"inferenceProfileName"`
				Status string `json:"status"`
				Models []struct {
					ARN string `json:"modelArn"`
				} `json:"models"`
			} `json:"inferenceProfileSummaries"`
			Next string `json:"nextToken"`
		}
		if err := get(path, &page); err != nil {
			return nil, err
		}
		for _, p := range page.Profiles {
			if p.Status != "ACTIVE" || p.ID == "" {
				continue
			}
			for _, reference := range p.Models {
				_, id, ok := strings.Cut(reference.ARN, "foundation-model/")
				m, found := byID[id]
				if !ok || !found {
					continue
				}
				m.ID, m.Name = p.ID, p.Name
				models = append(models, m)
				break
			}
		}
		if page.Next == "" {
			break
		}
		if seenTokens[page.Next] {
			return nil, fmt.Errorf("Bedrock returned a repeated inference profile page token")
		}
		seenTokens[page.Next] = true
		path = "/inference-profiles?maxResults=100&nextToken=" + url.QueryEscape(page.Next)
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	models = slices.CompactFunc(models, func(a, b ai.Model) bool { return a.ID == b.ID })
	return models, nil
}

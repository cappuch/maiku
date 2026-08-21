// Package google implements streaming against Google's Generative Language
// API (Gemini streamGenerateContent SSE). Pragmatic Go port of
// pi-ai's google-generative-ai.ts / google-shared.ts.
package google

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mikus/maiku/ai"
)

const (
	defaultBaseURL   = "https://generativelanguage.googleapis.com/v1beta"
	defaultMaxTokens = 8192
)

var toolCallCounter atomic.Uint64

func init() {
	ai.RegisterAPI(ai.APIGoogleGenerativeAI, Stream)
}

// ---- Wire types ----

type geminiPart struct {
	Text             string            `json:"text,omitempty"`
	Thought          bool              `json:"thought,omitempty"`
	ThoughtSignature string            `json:"thoughtSignature,omitempty"`
	InlineData       *inlineData       `json:"inlineData,omitempty"`
	FunctionCall     *functionCall     `json:"functionCall,omitempty"`
	FunctionResponse *functionResponse `json:"functionResponse,omitempty"`
}

type inlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type functionCall struct {
	ID   string         `json:"id,omitempty"`
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}

type functionResponse struct {
	ID       string         `json:"id,omitempty"`
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type functionDeclaration struct {
	Name                 string          `json:"name"`
	Description          string          `json:"description,omitempty"`
	ParametersJsonSchema json.RawMessage `json:"parametersJsonSchema,omitempty"`
}

type geminiTool struct {
	FunctionDeclarations []functionDeclaration `json:"functionDeclarations"`
}

type thinkingConfig struct {
	IncludeThoughts bool   `json:"includeThoughts,omitempty"`
	ThinkingBudget  *int   `json:"thinkingBudget,omitempty"`
	ThinkingLevel   string `json:"thinkingLevel,omitempty"`
}

type generationConfig struct {
	Temperature     *float64        `json:"temperature,omitempty"`
	MaxOutputTokens int             `json:"maxOutputTokens,omitempty"`
	ThinkingConfig  *thinkingConfig `json:"thinkingConfig,omitempty"`
}

type systemInstruction struct {
	Parts []geminiPart `json:"parts"`
}

type generateContentRequest struct {
	Contents          []geminiContent    `json:"contents"`
	SystemInstruction *systemInstruction `json:"systemInstruction,omitempty"`
	Tools             []geminiTool       `json:"tools,omitempty"`
	GenerationConfig  *generationConfig  `json:"generationConfig,omitempty"`
}

type usageMetadata struct {
	PromptTokenCount        int `json:"promptTokenCount"`
	CandidatesTokenCount    int `json:"candidatesTokenCount"`
	TotalTokenCount         int `json:"totalTokenCount"`
	CachedContentTokenCount int `json:"cachedContentTokenCount"`
	ThoughtsTokenCount      int `json:"thoughtsTokenCount"`
}

type streamCandidate struct {
	Content      *geminiContent `json:"content"`
	FinishReason string         `json:"finishReason"`
}

type generateContentResponse struct {
	ResponseID    string            `json:"responseId"`
	Candidates    []streamCandidate `json:"candidates"`
	UsageMetadata *usageMetadata    `json:"usageMetadata"`
}

// Stream implements ai.StreamFn for the Google Generative AI API.
func Stream(model ai.Model, ctx ai.Context, opts *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
	out := ai.NewAssistantMessageEventStream()
	go run(out, model, ctx, opts)
	return out
}

func run(out *ai.AssistantMessageEventStream, model ai.Model, ctxData ai.Context, opts *ai.SimpleStreamOptions) {
	if opts == nil {
		opts = &ai.SimpleStreamOptions{}
	}

	msg := ai.AssistantMessage{
		Role:       "assistant",
		Content:    []ai.AssistantContentBlock{},
		API:        ai.APIGoogleGenerativeAI,
		Provider:   model.Provider,
		Model:      model.ID,
		Usage:      ai.EmptyUsage(),
		StopReason: ai.StopPending,
		Timestamp:  time.Now().UnixMilli(),
	}

	fail := func(err error, aborted bool) {
		if aborted {
			msg.StopReason = ai.StopAborted
		} else {
			msg.StopReason = ai.StopError
		}
		msg.ErrorMessage = err.Error()
		out.Push(ai.AssistantMessageEvent{Type: "error", Reason: msg.StopReason, Error: &msg})
	}

	apiKey := opts.APIKey
	if apiKey == "" && !hasAuthHeader(opts.Headers) {
		fail(fmt.Errorf("no API key for provider: %s", model.Provider), false)
		return
	}

	reqBody := buildRequest(model, ctxData, opts)
	body, err := json.Marshal(reqBody)
	if err != nil {
		fail(fmt.Errorf("encode request: %w", err), false)
		return
	}

	baseURL := strings.TrimRight(model.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	endpoint := fmt.Sprintf("%s/models/%s:streamGenerateContent?alt=sse", baseURL, model.ID)

	httpCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if opts.TimeoutMs != nil {
		var timeoutCancel context.CancelFunc
		httpCtx, timeoutCancel = context.WithTimeout(httpCtx, time.Duration(*opts.TimeoutMs)*time.Millisecond)
		defer timeoutCancel()
	}
	aborted := false
	if opts.Signal != nil {
		go func() {
			select {
			case <-opts.Signal:
				aborted = true
				cancel()
			case <-httpCtx.Done():
			}
		}()
	}

	httpReq, err := http.NewRequestWithContext(httpCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		fail(fmt.Errorf("build http request: %w", err), false)
		return
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("accept", "text/event-stream")
	if apiKey != "" {
		httpReq.Header.Set("x-goog-api-key", apiKey)
	}
	for k, v := range model.Headers {
		httpReq.Header.Set(k, v)
	}
	for k, v := range opts.Headers {
		if v == nil {
			httpReq.Header.Del(k)
		} else {
			httpReq.Header.Set(k, *v)
		}
	}

	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		if aborted {
			fail(fmt.Errorf("request was aborted"), true)
		} else {
			fail(fmt.Errorf("google request failed: %w", err), false)
		}
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if opts.OnResponse != nil {
		hdrs := map[string]string{}
		for k := range resp.Header {
			hdrs[k] = resp.Header.Get(k)
		}
		opts.OnResponse(ai.ProviderResponse{Status: resp.StatusCode, Headers: hdrs}, model)
	}

	if resp.StatusCode >= 400 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		fail(fmt.Errorf("google API error (%d): %s", resp.StatusCode, sanitizeErrorBody(errBody)), false)
		return
	}

	out.Push(ai.AssistantMessageEvent{Type: "start", Partial: &msg})

	var currentBlockType string // "", "text", "thinking"
	var currentBlockIndex = -1
	hasFinishReason := false

	closeCurrentBlock := func() {
		if currentBlockIndex < 0 || currentBlockIndex >= len(msg.Content) {
			currentBlockType = ""
			currentBlockIndex = -1
			return
		}
		block := msg.Content[currentBlockIndex]
		switch currentBlockType {
		case "text":
			out.Push(ai.AssistantMessageEvent{Type: "text_end", ContentIndex: currentBlockIndex, Content: block.Text, Partial: &msg})
		case "thinking":
			out.Push(ai.AssistantMessageEvent{Type: "thinking_end", ContentIndex: currentBlockIndex, Content: block.Thinking, Partial: &msg})
		}
		currentBlockType = ""
		currentBlockIndex = -1
	}

	ensureBlock := func(kind string) int {
		if currentBlockType == kind && currentBlockIndex >= 0 {
			return currentBlockIndex
		}
		closeCurrentBlock()
		if kind == "thinking" {
			msg.Content = append(msg.Content, ai.ThinkingBlock(""))
			currentBlockType = "thinking"
		} else {
			msg.Content = append(msg.Content, ai.TextBlock(""))
			currentBlockType = "text"
		}
		currentBlockIndex = len(msg.Content) - 1
		eventType := "text_start"
		if kind == "thinking" {
			eventType = "thinking_start"
		}
		out.Push(ai.AssistantMessageEvent{Type: eventType, ContentIndex: currentBlockIndex, Partial: &msg})
		return currentBlockIndex
	}

	retainSig := func(existing, incoming string) string {
		if incoming != "" {
			return incoming
		}
		return existing
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	var scanErr error
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		name, value, found := strings.Cut(line, ":")
		if !found || name != "data" {
			continue
		}
		value = strings.TrimPrefix(value, " ")
		if value == "" || value == "[DONE]" {
			continue
		}

		var chunk generateContentResponse
		if err := json.Unmarshal([]byte(value), &chunk); err != nil {
			continue
		}

		if msg.ResponseID == "" && chunk.ResponseID != "" {
			msg.ResponseID = chunk.ResponseID
		}
		if chunk.UsageMetadata != nil {
			msg.Usage = parseUsage(*chunk.UsageMetadata, model)
		}

		if len(chunk.Candidates) == 0 {
			continue
		}
		cand := chunk.Candidates[0]

		if cand.FinishReason != "" {
			msg.RawStopReason = cand.FinishReason
			msg.StopReason = mapStopReason(cand.FinishReason)
			hasFinishReason = true
		}

		if cand.Content == nil {
			continue
		}
		for _, part := range cand.Content.Parts {
			if part.FunctionCall != nil {
				closeCurrentBlock()
				fc := part.FunctionCall
				toolCallID := fc.ID
				needsNewID := toolCallID == ""
				if !needsNewID {
					for _, b := range msg.Content {
						if b.Type == "toolCall" && b.ID == toolCallID {
							needsNewID = true
							break
						}
					}
				}
				if needsNewID {
					toolCallID = fmt.Sprintf("%s_%d_%d", fc.Name, time.Now().UnixMilli(), toolCallCounter.Add(1))
				}
				args := fc.Args
				if args == nil {
					args = map[string]any{}
				}
				tc := ai.ToolCall{
					Type:      "toolCall",
					ID:        toolCallID,
					Name:      fc.Name,
					Arguments: args,
				}
				if part.ThoughtSignature != "" {
					tc.ThoughtSignature = part.ThoughtSignature
				}
				msg.Content = append(msg.Content, ai.ToolCallBlock(tc))
				idx := len(msg.Content) - 1
				delta, _ := json.Marshal(args)
				out.Push(ai.AssistantMessageEvent{Type: "toolcall_start", ContentIndex: idx, Partial: &msg})
				out.Push(ai.AssistantMessageEvent{Type: "toolcall_delta", ContentIndex: idx, Delta: string(delta), Partial: &msg})
				out.Push(ai.AssistantMessageEvent{Type: "toolcall_end", ContentIndex: idx, ToolCall: &tc, Partial: &msg})
				continue
			}

			if part.Text == "" && part.ThoughtSignature == "" {
				continue
			}

			isThinking := part.Thought
			if isThinking {
				idx := ensureBlock("thinking")
				block := &msg.Content[idx]
				block.Thinking += part.Text
				block.ThinkingSignature = retainSig(block.ThinkingSignature, part.ThoughtSignature)
				if part.Text != "" {
					out.Push(ai.AssistantMessageEvent{Type: "thinking_delta", ContentIndex: idx, Delta: part.Text, Partial: &msg})
				}
			} else if part.Text != "" || part.ThoughtSignature != "" {
				idx := ensureBlock("text")
				block := &msg.Content[idx]
				block.Text += part.Text
				block.TextSignature = retainSig(block.TextSignature, part.ThoughtSignature)
				if part.Text != "" {
					out.Push(ai.AssistantMessageEvent{Type: "text_delta", ContentIndex: idx, Delta: part.Text, Partial: &msg})
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		scanErr = err
	}

	closeCurrentBlock()

	// Tool calls force toolUse stop reason even when finishReason is STOP.
	hasToolCall := false
	for _, b := range msg.Content {
		if b.Type == "toolCall" {
			hasToolCall = true
			break
		}
	}
	if hasToolCall && (msg.StopReason == ai.StopStop || msg.StopReason == ai.StopPending) {
		msg.StopReason = ai.StopToolUse
		hasFinishReason = true
	}

	if scanErr != nil {
		if aborted {
			fail(fmt.Errorf("request was aborted"), true)
		} else {
			fail(scanErr, false)
		}
		return
	}
	if aborted {
		fail(fmt.Errorf("request was aborted"), true)
		return
	}
	if !hasFinishReason {
		fail(fmt.Errorf("google stream ended without a finish reason"), false)
		return
	}
	if msg.StopReason == ai.StopError || msg.StopReason == ai.StopAborted {
		errMessage := msg.ErrorMessage
		if errMessage == "" {
			if msg.RawStopReason != "" {
				errMessage = fmt.Sprintf("provider stopped with: %s", msg.RawStopReason)
			} else {
				errMessage = "an unknown error occurred"
			}
		}
		fail(fmt.Errorf("%s", errMessage), msg.StopReason == ai.StopAborted)
		return
	}

	out.Push(ai.AssistantMessageEvent{Type: "done", Reason: msg.StopReason, Message: &msg})
}

func hasAuthHeader(headers map[string]*string) bool {
	for k, v := range headers {
		lk := strings.ToLower(k)
		if (lk == "authorization" || lk == "x-goog-api-key") && v != nil && strings.TrimSpace(*v) != "" {
			return true
		}
	}
	return false
}

func sanitizeErrorBody(b []byte) string {
	s := strings.TrimSpace(string(b))
	if s == "" {
		return "(empty body)"
	}
	if len(s) > 4000 {
		s = s[:4000] + "...(truncated)"
	}
	return s
}

func buildRequest(model ai.Model, ctxData ai.Context, opts *ai.SimpleStreamOptions) *generateContentRequest {
	req := &generateContentRequest{
		Contents: convertMessages(model, ctxData),
	}

	if ctxData.SystemPrompt != "" {
		req.SystemInstruction = &systemInstruction{
			Parts: []geminiPart{{Text: ctxData.SystemPrompt}},
		}
	}

	gen := &generationConfig{}
	maxTokens := model.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}
	if opts.MaxTokens != nil && *opts.MaxTokens > 0 {
		maxTokens = *opts.MaxTokens
	}
	gen.MaxOutputTokens = maxTokens
	if opts.Temperature != nil {
		gen.Temperature = opts.Temperature
	}

	thinkingEnabled := model.Reasoning && opts.Reasoning != "" && opts.Reasoning != ai.ThinkingOff
	if thinkingEnabled {
		budget := thinkingBudgetFor(model.ID, opts.Reasoning, opts.ThinkingBudgets)
		gen.ThinkingConfig = &thinkingConfig{
			IncludeThoughts: true,
			ThinkingBudget:  &budget,
		}
		// Gemini counts thoughts toward maxOutputTokens — expand so the
		// answer allotment stays intact.
		gen.MaxOutputTokens = ai.ExpandMaxTokensForThinking(maxTokens, opts.Reasoning, opts.ThinkingBudgets)
	} else if model.Reasoning {
		zero := 0
		gen.ThinkingConfig = &thinkingConfig{ThinkingBudget: &zero}
	}

	req.GenerationConfig = gen

	if len(ctxData.Tools) > 0 {
		decls := make([]functionDeclaration, 0, len(ctxData.Tools))
		for _, t := range ctxData.Tools {
			params := t.Parameters
			if len(params) == 0 {
				params = json.RawMessage(`{"type":"object","properties":{}}`)
			}
			decls = append(decls, functionDeclaration{
				Name:                 t.Name,
				Description:          t.Description,
				ParametersJsonSchema: params,
			})
		}
		req.Tools = []geminiTool{{FunctionDeclarations: decls}}
	}

	return req
}

func thinkingBudgetFor(modelID string, level ai.ThinkingLevel, custom *ai.ThinkingBudgets) int {
	if custom != nil {
		switch level {
		case ai.ThinkingMinimal:
			if custom.Minimal != nil {
				return *custom.Minimal
			}
		case ai.ThinkingLow:
			if custom.Low != nil {
				return *custom.Low
			}
		case ai.ThinkingMedium:
			if custom.Medium != nil {
				return *custom.Medium
			}
		case ai.ThinkingHigh, ai.ThinkingXHigh, ai.ThinkingMax:
			if custom.High != nil {
				return *custom.High
			}
		}
	}

	id := strings.ToLower(modelID)
	var budgets map[ai.ThinkingLevel]int
	switch {
	case strings.Contains(id, "2.5-pro"):
		budgets = map[ai.ThinkingLevel]int{
			ai.ThinkingMinimal: 128,
			ai.ThinkingLow:     2048,
			ai.ThinkingMedium:  8192,
			ai.ThinkingHigh:    32768,
			ai.ThinkingXHigh:   32768,
			ai.ThinkingMax:     32768,
		}
	case strings.Contains(id, "2.5-flash-lite"):
		budgets = map[ai.ThinkingLevel]int{
			ai.ThinkingMinimal: 512,
			ai.ThinkingLow:     2048,
			ai.ThinkingMedium:  8192,
			ai.ThinkingHigh:    24576,
			ai.ThinkingXHigh:   24576,
			ai.ThinkingMax:     24576,
		}
	case strings.Contains(id, "2.5-flash"):
		budgets = map[ai.ThinkingLevel]int{
			ai.ThinkingMinimal: 128,
			ai.ThinkingLow:     2048,
			ai.ThinkingMedium:  8192,
			ai.ThinkingHigh:    24576,
			ai.ThinkingXHigh:   24576,
			ai.ThinkingMax:     24576,
		}
	default:
		return -1 // dynamic
	}
	if b, ok := budgets[level]; ok {
		return b
	}
	return budgets[ai.ThinkingMedium]
}

func convertMessages(model ai.Model, ctxData ai.Context) []geminiContent {
	var out []geminiContent
	supportsImage := slices.Contains(model.Input, "image")

	for i := 0; i < len(ctxData.Messages); i++ {
		m := ctxData.Messages[i]
		switch m.Role {
		case "user":
			parts := userParts(m.UserContent)
			if len(parts) == 0 {
				continue
			}
			out = append(out, geminiContent{Role: "user", Parts: parts})

		case "assistant":
			same := m.Provider == model.Provider && m.Model == model.ID
			var parts []geminiPart
			for _, b := range m.AssistantContent {
				switch b.Type {
				case "text":
					sig := resolveThoughtSignature(same, b.TextSignature)
					if strings.TrimSpace(b.Text) == "" && sig == "" {
						continue
					}
					p := geminiPart{Text: b.Text}
					if sig != "" {
						p.ThoughtSignature = sig
					}
					parts = append(parts, p)
				case "thinking":
					if same {
						sig := resolveThoughtSignature(same, b.ThinkingSignature)
						if strings.TrimSpace(b.Thinking) == "" && sig == "" {
							continue
						}
						p := geminiPart{Thought: true, Text: b.Thinking}
						if sig != "" {
							p.ThoughtSignature = sig
						}
						parts = append(parts, p)
					} else {
						if strings.TrimSpace(b.Thinking) == "" {
							continue
						}
						parts = append(parts, geminiPart{Text: b.Thinking})
					}
				case "toolCall":
					args := b.Arguments
					if args == nil {
						args = map[string]any{}
					}
					fc := &functionCall{Name: b.Name, Args: args}
					if requiresToolCallID(model.ID) {
						fc.ID = b.ID
					}
					p := geminiPart{FunctionCall: fc}
					if sig := resolveThoughtSignature(same, b.ThoughtSignature); sig != "" {
						p.ThoughtSignature = sig
					}
					parts = append(parts, p)
				}
			}
			if len(parts) == 0 {
				continue
			}
			out = append(out, geminiContent{Role: "model", Parts: parts})

		case "toolResult":
			var texts []string
			var images []geminiPart
			for _, c := range m.ToolContent {
				switch c.Type {
				case "image":
					if supportsImage {
						images = append(images, geminiPart{InlineData: &inlineData{MimeType: c.MimeType, Data: c.Data}})
					}
				default:
					if c.Text != "" {
						texts = append(texts, c.Text)
					}
				}
			}
			textResult := strings.Join(texts, "\n")
			responseValue := textResult
			if responseValue == "" {
				if len(images) > 0 {
					responseValue = "(see attached image)"
				}
			}
			response := map[string]any{"output": responseValue}
			if m.IsError {
				response = map[string]any{"error": responseValue}
			}
			fr := &functionResponse{Name: m.ToolName, Response: response}
			if requiresToolCallID(model.ID) {
				fr.ID = m.ToolCallID
			}
			frPart := geminiPart{FunctionResponse: fr}

			if len(out) > 0 {
				last := &out[len(out)-1]
				hasFR := false
				if last.Role == "user" {
					for _, p := range last.Parts {
						if p.FunctionResponse != nil {
							hasFR = true
							break
						}
					}
				}
				if hasFR {
					last.Parts = append(last.Parts, frPart)
				} else {
					out = append(out, geminiContent{Role: "user", Parts: []geminiPart{frPart}})
				}
			} else {
				out = append(out, geminiContent{Role: "user", Parts: []geminiPart{frPart}})
			}

			// Gemini < 3: images as a follow-up user turn.
			if len(images) > 0 && !supportsMultimodalFunctionResponse(model.ID) {
				parts := append([]geminiPart{{Text: "Tool result image:"}}, images...)
				out = append(out, geminiContent{Role: "user", Parts: parts})
			}
		}
	}
	return out
}

func userParts(content any) []geminiPart {
	switch v := content.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return []geminiPart{{Text: v}}
	case []ai.TextContent:
		var parts []geminiPart
		for _, t := range v {
			if strings.TrimSpace(t.Text) == "" {
				continue
			}
			parts = append(parts, geminiPart{Text: t.Text})
		}
		return parts
	case []any:
		var parts []geminiPart
		for _, item := range v {
			switch item := item.(type) {
			case string:
				if strings.TrimSpace(item) == "" {
					continue
				}
				parts = append(parts, geminiPart{Text: item})
			case ai.TextContent:
				if strings.TrimSpace(item.Text) == "" {
					continue
				}
				parts = append(parts, geminiPart{Text: item.Text})
			case ai.ImageContent:
				parts = append(parts, geminiPart{InlineData: &inlineData{MimeType: item.MimeType, Data: item.Data}})
			case map[string]any:
				switch item["type"] {
				case "text":
					text, _ := item["text"].(string)
					if strings.TrimSpace(text) == "" {
						continue
					}
					parts = append(parts, geminiPart{Text: text})
				case "image":
					data, _ := item["data"].(string)
					mimeType, _ := item["mimeType"].(string)
					parts = append(parts, geminiPart{InlineData: &inlineData{MimeType: mimeType, Data: data}})
				}
			}
		}
		return parts
	default:
		return nil
	}
}

func requiresToolCallID(modelID string) bool {
	v := geminiMajorVersion(modelID)
	return v != nil && *v >= 3
}

func supportsMultimodalFunctionResponse(modelID string) bool {
	v := geminiMajorVersion(modelID)
	if v != nil {
		return *v >= 3
	}
	return true
}

func geminiMajorVersion(modelID string) *int {
	id := strings.ToLower(modelID)
	if !strings.HasPrefix(id, "gemini") {
		return nil
	}
	// gemini-2.5-pro, gemini-3-flash, gemini-live-2.5-...
	rest := strings.TrimPrefix(id, "gemini-live-")
	rest = strings.TrimPrefix(rest, "gemini-")
	if rest == id {
		return nil
	}
	majorStr := rest
	if i := strings.IndexAny(rest, ".-"); i >= 0 {
		majorStr = rest[:i]
	}
	var major int
	if _, err := fmt.Sscanf(majorStr, "%d", &major); err != nil {
		return nil
	}
	return &major
}

func resolveThoughtSignature(sameProviderAndModel bool, signature string) string {
	if !sameProviderAndModel || signature == "" {
		return ""
	}
	if len(signature)%4 != 0 {
		return ""
	}
	for _, c := range signature {
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '+' || c == '/' || c == '=' {
			continue
		}
		return ""
	}
	return signature
}

func parseUsage(u usageMetadata, model ai.Model) ai.Usage {
	cacheRead := u.CachedContentTokenCount
	input := max(u.PromptTokenCount-cacheRead, 0)
	output := u.CandidatesTokenCount + u.ThoughtsTokenCount
	usage := ai.Usage{
		Input:     input,
		Output:    output,
		CacheRead: cacheRead,
	}
	if u.ThoughtsTokenCount > 0 {
		r := u.ThoughtsTokenCount
		usage.Reasoning = &r
	}
	if u.TotalTokenCount > 0 {
		usage.TotalTokens = u.TotalTokenCount
	} else {
		usage.TotalTokens = usage.Input + usage.Output + usage.CacheRead + usage.CacheWrite
	}
	usage.Cost = ai.CalculateCost(model.Cost, usage)
	return usage
}

func mapStopReason(reason string) ai.StopReason {
	switch reason {
	case "STOP":
		return ai.StopStop
	case "MAX_TOKENS":
		return ai.StopLength
	default:
		return ai.StopError
	}
}

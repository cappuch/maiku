// Package anthropic implements streaming against Anthropic's /v1/messages API
// (SSE), converting to/from the shared ai.Context / ai.AssistantMessageEvent
// contract. It is a pragmatic Go port of pi-ai's anthropic-messages.ts.
package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/mikus/maiku/ai"
)

const (
	defaultBaseURL   = "https://api.anthropic.com"
	anthropicVersion = "2023-06-01"
	defaultMaxTokens = 8192
)

func init() {
	ai.RegisterAPI(ai.APIAnthropicMessages, Stream)
}

// ---- Wire types (subset of the Anthropic Messages API we need) ----

type anthropicTextBlock struct {
	Type         string          `json:"type"`
	Text         string          `json:"text,omitempty"`
	CacheControl json.RawMessage `json:"cache_control,omitempty"`
}

type anthropicImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type anthropicImageBlock struct {
	Type         string               `json:"type"`
	Source       anthropicImageSource `json:"source"`
	CacheControl json.RawMessage      `json:"cache_control,omitempty"`
}

type anthropicThinkingBlock struct {
	Type      string `json:"type"`
	Thinking  string `json:"thinking"`
	Signature string `json:"signature"`
}

type anthropicRedactedThinkingBlock struct {
	Type string `json:"type"`
	Data string `json:"data"`
}

type anthropicToolUseBlock struct {
	Type  string         `json:"type"`
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

type anthropicToolResultBlock struct {
	Type         string          `json:"type"`
	ToolUseID    string          `json:"tool_use_id"`
	Content      any             `json:"content"`
	IsError      bool            `json:"is_error,omitempty"`
	CacheControl json.RawMessage `json:"cache_control,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type anthropicTool struct {
	Name         string          `json:"name"`
	Description  string          `json:"description,omitempty"`
	InputSchema  json.RawMessage `json:"input_schema"`
	CacheControl json.RawMessage `json:"cache_control,omitempty"`
}

type anthropicThinkingConfig struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
}

type anthropicRequest struct {
	Model       string                   `json:"model"`
	Messages    []anthropicMessage       `json:"messages"`
	System      []anthropicTextBlock     `json:"system,omitempty"`
	MaxTokens   int                      `json:"max_tokens"`
	Stream      bool                     `json:"stream"`
	Temperature *float64                 `json:"temperature,omitempty"`
	Tools       []anthropicTool          `json:"tools,omitempty"`
	Thinking    *anthropicThinkingConfig `json:"thinking,omitempty"`
}

// Stream implements ai.StreamFn for the Anthropic Messages API.
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
		API:        ai.APIAnthropicMessages,
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

	req, err := buildRequest(model, ctxData, opts)
	if err != nil {
		fail(err, false)
		return
	}

	baseURL := model.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	body, err := json.Marshal(req)
	if err != nil {
		fail(fmt.Errorf("encode request: %w", err), false)
		return
	}

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

	httpReq, err := http.NewRequestWithContext(httpCtx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		fail(fmt.Errorf("build http request: %w", err), false)
		return
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("accept", "text/event-stream")
	httpReq.Header.Set("anthropic-version", anthropicVersion)
	if apiKey != "" {
		if strings.Contains(apiKey, "sk-ant-oat") {
			httpReq.Header.Set("authorization", "Bearer "+apiKey)
		} else {
			httpReq.Header.Set("x-api-key", apiKey)
		}
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
			fail(fmt.Errorf("anthropic request failed: %w", err), false)
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
		fail(fmt.Errorf("anthropic API error (%d): %s", resp.StatusCode, sanitizeErrorBody(errBody)), false)
		return
	}

	out.Push(ai.AssistantMessageEvent{Type: "start", Partial: &msg})

	blocks := map[int]*blockState{}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	var event string
	var dataLines []string
	sawMessageStart := false
	sawMessageStop := false

	flush := func() error {
		if event == "" && len(dataLines) == 0 {
			return nil
		}
		data := strings.Join(dataLines, "\n")
		ev := event
		event = ""
		dataLines = nil
		if ev == "" {
			ev = "message" // Anthropic always sets event: field; default no-op otherwise.
		}
		if ev == "error" {
			return fmt.Errorf("%s", data)
		}
		if ev != "message_start" && ev != "message_delta" && ev != "message_stop" &&
			ev != "content_block_start" && ev != "content_block_delta" && ev != "content_block_stop" {
			return nil
		}

		var raw map[string]any
		if err := json.Unmarshal([]byte(data), &raw); err != nil {
			return fmt.Errorf("could not parse Anthropic SSE event %s: %w; data=%s", ev, err, data)
		}

		switch ev {
		case "message_start":
			sawMessageStart = true
			handleMessageStart(&msg, raw, model)
		case "content_block_start":
			handleContentBlockStart(out, &msg, blocks, raw)
		case "content_block_delta":
			handleContentBlockDelta(out, &msg, blocks, raw)
		case "content_block_stop":
			handleContentBlockStop(out, &msg, blocks, raw)
		case "message_delta":
			handleMessageDelta(&msg, raw, model)
		case "message_stop":
			sawMessageStop = true
		}
		return nil
	}

	var scanErr error
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" {
			if err := flush(); err != nil {
				scanErr = err
				break
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		name, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch name {
		case "event":
			event = value
		case "data":
			dataLines = append(dataLines, value)
		}
	}
	if scanErr == nil {
		if err := scanner.Err(); err != nil {
			scanErr = err
		} else if err := flush(); err != nil {
			scanErr = err
		}
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
	if !sawMessageStop && sawMessageStart {
		fail(fmt.Errorf("anthropic stream ended before message_stop"), false)
		return
	}
	if msg.StopReason == ai.StopPending {
		fail(fmt.Errorf("anthropic stream ended without a stop reason"), false)
		return
	}
	if msg.StopReason == ai.StopError || msg.StopReason == ai.StopAborted {
		errMessage := msg.ErrorMessage
		if errMessage == "" {
			errMessage = "an unknown error occurred"
		}
		fail(fmt.Errorf("%s", errMessage), msg.StopReason == ai.StopAborted)
		return
	}

	out.Push(ai.AssistantMessageEvent{Type: "done", Reason: msg.StopReason, Message: &msg})
}

func hasAuthHeader(headers map[string]*string) bool {
	for k, v := range headers {
		lk := strings.ToLower(k)
		if (lk == "authorization" || lk == "x-api-key") && v != nil && strings.TrimSpace(*v) != "" {
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

// ---- Request building ----

// resolveCacheControl picks the cache_control marker for this request.
// Prompt caching is always on unless callers opt out (CacheNone, e.g.
// compaction summaries).
//
// Default retention: "long" (1h TTL) on endpoints known to accept the ttl
// extension — agent turns routinely idle longer than the 5-minute window,
// and a full-prefix rewrite costs far more than the pricier one-hour write.
// Everything else (explicit CacheShort, or third-party
// anthropic-compatible providers without a Compat opt-in) gets the plain
// ephemeral marker, which carries the provider's default TTL.
func resolveCacheControl(model ai.Model, opts *ai.SimpleStreamOptions) (json.RawMessage, bool) {
	retention := opts.CacheRetention
	if retention == "" {
		// Unset defaults to 1h on endpoints that accept the ttl extension;
		// explicit choices below always win.
		retention = ai.CacheLong
	}
	if retention == ai.CacheNone {
		return nil, false
	}
	if retention == ai.CacheLong && !supportsLongCacheRetention(model) {
		retention = ai.CacheShort // providers that reject ttl get the default TTL
	}
	return json.RawMessage(`{"type":"ephemeral"` + cacheTTL(retention) + `}`), true
}

func cacheTTL(retention ai.CacheRetention) string {
	if retention == ai.CacheLong {
		return `,"ttl":"1h"`
	}
	return "" // default 5-minute TTL
}

// supportsLongCacheRetention gates the `ttl:"1h"` extension.
//
// First-party Anthropic endpoints accept it. Some OpenAI-compatible or
// anthropic-compatible proxies reject the extra field outright, so unknown
// providers default to NO until they opt in via
// Compat["supportsLongCacheRetention"]=true (or out with =false).
func supportsLongCacheRetention(model ai.Model) bool {
	if v, ok := model.Compat["supportsLongCacheRetention"].(bool); ok {
		return v
	}
	return isFirstPartyAnthropicEndpoint(model)
}

// isFirstPartyAnthropicEndpoint reports whether this model talks directly to
// Anthropic's own API (provider id "anthropic" on the default or official
// base URL), as opposed to a third-party service exposing a compatible wire
// format.
func isFirstPartyAnthropicEndpoint(model ai.Model) bool {
	if model.Provider != "anthropic" {
		return false
	}
	base := strings.TrimRight(model.BaseURL, "/")
	return base == "" || base == defaultBaseURL
}

func buildRequest(model ai.Model, ctxData ai.Context, opts *ai.SimpleStreamOptions) (*anthropicRequest, error) {
	cacheControl, cacheEnabled := resolveCacheControl(model, opts)
	messages, err := convertMessages(ctxData.Messages, cacheControl, cacheEnabled)
	if err != nil {
		return nil, err
	}

	maxTokens := model.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}
	if opts.MaxTokens != nil && *opts.MaxTokens > 0 {
		maxTokens = *opts.MaxTokens
	}

	req := &anthropicRequest{
		Model:     model.ID,
		Messages:  messages,
		MaxTokens: maxTokens,
		Stream:    true,
	}

	if ctxData.SystemPrompt != "" {
		system := anthropicTextBlock{Type: "text", Text: ctxData.SystemPrompt}
		if cacheEnabled {
			system.CacheControl = cacheControl
		}
		req.System = []anthropicTextBlock{system}
	}

	thinkingEnabled := model.Reasoning && opts.Reasoning != "" && opts.Reasoning != ai.ThinkingOff
	if opts.Temperature != nil && !thinkingEnabled {
		req.Temperature = opts.Temperature
	}

	if len(ctxData.Tools) > 0 {
		tools := make([]anthropicTool, 0, len(ctxData.Tools))
		for i, t := range ctxData.Tools {
			params := t.Parameters
			if len(params) == 0 {
				params = json.RawMessage(`{"type":"object","properties":{}}`)
			}
			tool := anthropicTool{
				Name:        t.Name,
				Description: t.Description,
				InputSchema: params,
			}
			// A breakpoint on the LAST tool definition pins the whole tool
			// section so schemas are cache-read back even after the transcript
			// prefix changes (compaction, new turns) instead of rewriting.
			if cacheEnabled && i == len(ctxData.Tools)-1 {
				tool.CacheControl = cacheControl
			}
			tools = append(tools, tool)
		}
		req.Tools = tools
	}

	if thinkingEnabled {
		budget := max(ai.ThinkingBudgetFor(opts.Reasoning, opts.ThinkingBudgets), 1024)
		// Thinking tokens count against max_tokens — add the budget on top of
		// the answer allotment so reasoning cannot starve the reply.
		req.MaxTokens = max(ai.ExpandMaxTokensForThinking(maxTokens, opts.Reasoning, opts.ThinkingBudgets), budget+1024)
		req.Thinking = &anthropicThinkingConfig{Type: "enabled", BudgetTokens: budget}
	} else if model.Reasoning {
		req.Thinking = &anthropicThinkingConfig{Type: "disabled"}
	}

	return req, nil
}

var toolCallIDSanitizer = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

func normalizeToolCallID(id string) string {
	id = toolCallIDSanitizer.ReplaceAllString(id, "_")
	if len(id) > 64 {
		id = id[:64]
	}
	return id
}

func convertMessages(messages []ai.Message, cacheControl json.RawMessage, cacheEnabled bool) ([]anthropicMessage, error) {
	var out []anthropicMessage

	for i := 0; i < len(messages); i++ {
		m := messages[i]
		switch m.Role {
		case "user":
			blocks, ok := userContentBlocks(m.UserContent)
			if !ok || len(blocks) == 0 {
				continue
			}
			out = append(out, anthropicMessage{Role: "user", Content: blocks})

		case "assistant":
			var blocks []any
			for _, b := range m.AssistantContent {
				switch b.Type {
				case "text":
					if strings.TrimSpace(b.Text) == "" {
						continue
					}
					blocks = append(blocks, anthropicTextBlock{Type: "text", Text: b.Text})
				case "thinking":
					if b.Redacted {
						blocks = append(blocks, anthropicRedactedThinkingBlock{Type: "redacted_thinking", Data: b.ThinkingSignature})
						continue
					}
					if strings.TrimSpace(b.Thinking) == "" && strings.TrimSpace(b.ThinkingSignature) == "" {
						continue
					}
					if strings.TrimSpace(b.ThinkingSignature) == "" {
						// No signature (e.g. from an aborted stream): Anthropic rejects
						// thinking blocks without one, so replay as plain text instead.
						blocks = append(blocks, anthropicTextBlock{Type: "text", Text: b.Thinking})
						continue
					}
					blocks = append(blocks, anthropicThinkingBlock{Type: "thinking", Thinking: b.Thinking, Signature: b.ThinkingSignature})
				case "toolCall":
					args := b.Arguments
					if args == nil {
						args = map[string]any{}
					}
					blocks = append(blocks, anthropicToolUseBlock{
						Type:  "tool_use",
						ID:    normalizeToolCallID(b.ID),
						Name:  b.Name,
						Input: args,
					})
				}
			}
			if len(blocks) == 0 {
				continue
			}
			out = append(out, anthropicMessage{Role: "assistant", Content: blocks})

		case "toolResult":
			var toolResults []any
			j := i
			for j < len(messages) && messages[j].Role == "toolResult" {
				tr := messages[j]
				toolResults = append(toolResults, anthropicToolResultBlock{
					Type:      "tool_result",
					ToolUseID: normalizeToolCallID(tr.ToolCallID),
					Content:   toolResultContentBlocks(tr.ToolContent),
					IsError:   tr.IsError,
				})
				j++
			}
			i = j - 1
			out = append(out, anthropicMessage{Role: "user", Content: toolResults})
		}
	}

	// Rolling breakpoint on the last conversation block that can carry a
	// marker: the newest user turn (or tool-result turn) is always written;
	// everything before it is cache-read back, so cache hits climb with the
	// transcript instead of rewriting the whole prefix every turn.
	//
	// The marker goes on the LAST USER-ROLE message even when assistant
	// turns follow it: Anthropic matches on the longest shared prefix, so
	// moving the breakpoint forward would force a fresh write of those
	// blocks every request while gaining nothing. Walking back (instead of
	// only inspecting the final message) keeps the guarantee when filtering
	// drops empty trailing turns and the converted list ends non-user.
	if cacheEnabled && len(out) > 0 {
		for i := len(out) - 1; i >= 0; i-- {
			if out[i].Role != "user" {
				continue
			}
			if blocks, ok := out[i].Content.([]any); ok && len(blocks) > 0 {
				switch b := blocks[len(blocks)-1].(type) {
				case anthropicTextBlock:
					b.CacheControl = cacheControl
					blocks[len(blocks)-1] = b
				case anthropicImageBlock:
					b.CacheControl = cacheControl
					blocks[len(blocks)-1] = b
				case anthropicToolResultBlock:
					b.CacheControl = cacheControl
					blocks[len(blocks)-1] = b
				}
			}
			break // at most one rolling message marker
		}
	}

	return out, nil
}

func userContentBlocks(content any) ([]any, bool) {
	switch v := content.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return nil, false
		}
		return []any{anthropicTextBlock{Type: "text", Text: v}}, true
	case []ai.TextContent:
		var blocks []any
		for _, t := range v {
			if strings.TrimSpace(t.Text) == "" {
				continue
			}
			blocks = append(blocks, anthropicTextBlock{Type: "text", Text: t.Text})
		}
		return blocks, len(blocks) > 0
	case []any:
		var blocks []any
		for _, item := range v {
			switch item := item.(type) {
			case string:
				if strings.TrimSpace(item) == "" {
					continue
				}
				blocks = append(blocks, anthropicTextBlock{Type: "text", Text: item})
			case ai.TextContent:
				if strings.TrimSpace(item.Text) == "" {
					continue
				}
				blocks = append(blocks, anthropicTextBlock{Type: "text", Text: item.Text})
			case ai.ImageContent:
				blocks = append(blocks, anthropicImageBlock{
					Type:   "image",
					Source: anthropicImageSource{Type: "base64", MediaType: item.MimeType, Data: item.Data},
				})
			case map[string]any:
				switch item["type"] {
				case "text":
					text, _ := item["text"].(string)
					if strings.TrimSpace(text) == "" {
						continue
					}
					blocks = append(blocks, anthropicTextBlock{Type: "text", Text: text})
				case "image":
					data, _ := item["data"].(string)
					mimeType, _ := item["mimeType"].(string)
					blocks = append(blocks, anthropicImageBlock{
						Type:   "image",
						Source: anthropicImageSource{Type: "base64", MediaType: mimeType, Data: data},
					})
				}
			}
		}
		return blocks, len(blocks) > 0
	default:
		return nil, false
	}
}

func toolResultContentBlocks(content []ai.ToolResultContent) []any {
	var blocks []any
	for _, c := range content {
		switch c.Type {
		case "image":
			blocks = append(blocks, anthropicImageBlock{
				Type:   "image",
				Source: anthropicImageSource{Type: "base64", MediaType: c.MimeType, Data: c.Data},
			})
		default:
			if strings.TrimSpace(c.Text) == "" {
				continue
			}
			blocks = append(blocks, anthropicTextBlock{Type: "text", Text: c.Text})
		}
	}
	if len(blocks) == 0 {
		blocks = append(blocks, anthropicTextBlock{Type: "text", Text: "(no tool output)"})
	}
	return blocks
}

// ---- SSE event handling ----

func handleMessageStart(msg *ai.AssistantMessage, raw map[string]any, model ai.Model) {
	message, _ := raw["message"].(map[string]any)
	if message == nil {
		return
	}
	if id, ok := message["id"].(string); ok {
		msg.ResponseID = id
	}
	usage, _ := message["usage"].(map[string]any)
	applyUsage(msg, usage, model)
}

func applyUsage(msg *ai.AssistantMessage, usage map[string]any, model ai.Model) {
	if usage == nil {
		return
	}
	if v, ok := numberField(usage, "input_tokens"); ok {
		msg.Usage.Input = v
	}
	if v, ok := numberField(usage, "output_tokens"); ok {
		msg.Usage.Output = v
	}
	if v, ok := numberField(usage, "cache_read_input_tokens"); ok {
		msg.Usage.CacheRead = v
	}
	if v, ok := numberField(usage, "cache_creation_input_tokens"); ok {
		msg.Usage.CacheWrite = v
	}
	if cc, ok := usage["cache_creation"].(map[string]any); ok {
		if v, ok := numberField(cc, "ephemeral_1h_input_tokens"); ok {
			msg.Usage.CacheWrite1h = &v
		}
	}
	if details, ok := usage["output_tokens_details"].(map[string]any); ok {
		if v, ok := numberField(details, "thinking_tokens"); ok {
			msg.Usage.Reasoning = &v
		}
	}
	msg.Usage.TotalTokens = msg.Usage.Input + msg.Usage.Output + msg.Usage.CacheRead + msg.Usage.CacheWrite
	msg.Usage.Cost = ai.CalculateCost(model.Cost, msg.Usage)
}

func numberField(m map[string]any, key string) (int, bool) {
	v, ok := m[key]
	if !ok || v == nil {
		return 0, false
	}
	f, ok := v.(float64)
	if !ok {
		return 0, false
	}
	return int(f), true
}

// blockState tracks the mapping from an Anthropic content_block index to the
// corresponding index in msg.Content, plus the raw JSON accumulated so far
// for streaming tool-call arguments.
type blockState struct {
	index       int
	partialJSON string
}

func handleContentBlockStart(out *ai.AssistantMessageEventStream, msg *ai.AssistantMessage, blocks map[int]*blockState, raw map[string]any) {
	idxF, _ := raw["index"].(float64)
	anthropicIndex := int(idxF)
	cb, _ := raw["content_block"].(map[string]any)
	if cb == nil {
		return
	}
	cbType, _ := cb["type"].(string)

	var newBlock ai.AssistantContentBlock
	var eventType string
	switch cbType {
	case "text":
		text, _ := cb["text"].(string)
		newBlock = ai.TextBlock(text)
		eventType = "text_start"
	case "thinking":
		thinking, _ := cb["thinking"].(string)
		newBlock = ai.ThinkingBlock(thinking)
		if sig, ok := cb["signature"].(string); ok {
			newBlock.ThinkingSignature = sig
		}
		eventType = "thinking_start"
	case "redacted_thinking":
		data, _ := cb["data"].(string)
		newBlock = ai.AssistantContentBlock{Type: "thinking", Thinking: "[Reasoning redacted]", ThinkingSignature: data, Redacted: true}
		eventType = "thinking_start"
	case "tool_use":
		id, _ := cb["id"].(string)
		name, _ := cb["name"].(string)
		args := map[string]any{}
		if input, ok := cb["input"].(map[string]any); ok {
			args = input
		}
		newBlock = ai.AssistantContentBlock{Type: "toolCall", ID: id, Name: name, Arguments: args}
		eventType = "toolcall_start"
	default:
		return
	}

	msg.Content = append(msg.Content, newBlock)
	contentIndex := len(msg.Content) - 1
	blocks[anthropicIndex] = &blockState{index: contentIndex}

	out.Push(ai.AssistantMessageEvent{Type: eventType, ContentIndex: contentIndex, Partial: msg})
}

func handleContentBlockDelta(out *ai.AssistantMessageEventStream, msg *ai.AssistantMessage, blocks map[int]*blockState, raw map[string]any) {
	idxF, _ := raw["index"].(float64)
	anthropicIndex := int(idxF)
	b, ok := blocks[anthropicIndex]
	if !ok || b.index >= len(msg.Content) {
		return
	}
	delta, _ := raw["delta"].(map[string]any)
	if delta == nil {
		return
	}
	deltaType, _ := delta["type"].(string)
	block := &msg.Content[b.index]

	switch deltaType {
	case "text_delta":
		if block.Type != "text" {
			return
		}
		text, _ := delta["text"].(string)
		block.Text += text
		out.Push(ai.AssistantMessageEvent{Type: "text_delta", ContentIndex: b.index, Delta: text, Partial: msg})
	case "thinking_delta":
		if block.Type != "thinking" {
			return
		}
		thinking, _ := delta["thinking"].(string)
		block.Thinking += thinking
		out.Push(ai.AssistantMessageEvent{Type: "thinking_delta", ContentIndex: b.index, Delta: thinking, Partial: msg})
	case "input_json_delta":
		if block.Type != "toolCall" {
			return
		}
		partial, _ := delta["partial_json"].(string)
		b.partialJSON += partial
		block.Arguments = parseStreamingJSON(b.partialJSON)
		out.Push(ai.AssistantMessageEvent{Type: "toolcall_delta", ContentIndex: b.index, Delta: partial, Partial: msg})
	case "signature_delta":
		if block.Type != "thinking" {
			return
		}
		sig, _ := delta["signature"].(string)
		block.ThinkingSignature += sig
	}
}

func handleContentBlockStop(out *ai.AssistantMessageEventStream, msg *ai.AssistantMessage, blocks map[int]*blockState, raw map[string]any) {
	idxF, _ := raw["index"].(float64)
	anthropicIndex := int(idxF)
	b, ok := blocks[anthropicIndex]
	if !ok || b.index >= len(msg.Content) {
		return
	}
	block := &msg.Content[b.index]
	switch block.Type {
	case "text":
		out.Push(ai.AssistantMessageEvent{Type: "text_end", ContentIndex: b.index, Content: block.Text, Partial: msg})
	case "thinking":
		out.Push(ai.AssistantMessageEvent{Type: "thinking_end", ContentIndex: b.index, Content: block.Thinking, Partial: msg})
	case "toolCall":
		block.Arguments = parseStreamingJSON(b.partialJSON)
		tc, _ := block.AsToolCall()
		out.Push(ai.AssistantMessageEvent{Type: "toolcall_end", ContentIndex: b.index, ToolCall: &tc, Partial: msg})
	}
}

func handleMessageDelta(msg *ai.AssistantMessage, raw map[string]any, model ai.Model) {
	delta, _ := raw["delta"].(map[string]any)
	if delta != nil {
		if reason, ok := delta["stop_reason"].(string); ok && reason != "" {
			msg.RawStopReason = reason
			stopReason, errMessage := mapStopReason(reason, delta["stop_details"])
			msg.StopReason = stopReason
			if errMessage != "" {
				msg.ErrorMessage = errMessage
			}
		}
	}
	if usage, ok := raw["usage"].(map[string]any); ok {
		applyUsage(msg, usage, model)
	}
}

func mapStopReason(reason string, stopDetails any) (ai.StopReason, string) {
	switch reason {
	case "end_turn", "pause_turn", "stop_sequence":
		return ai.StopStop, ""
	case "max_tokens":
		return ai.StopLength, ""
	case "tool_use":
		return ai.StopToolUse, ""
	case "refusal":
		msg := "the model refused to complete the request"
		if details, ok := stopDetails.(map[string]any); ok {
			if explanation, ok := details["explanation"].(string); ok && explanation != "" {
				msg = explanation
			}
		}
		return ai.StopError, msg
	default:
		return ai.StopError, fmt.Sprintf("unhandled stop reason: %s", reason)
	}
}

// parseStreamingJSON best-effort parses a (possibly incomplete) JSON object
// produced by incremental tool-call argument streaming.
func parseStreamingJSON(partial string) map[string]any {
	if strings.TrimSpace(partial) == "" {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(partial), &out); err == nil {
		return out
	}
	repaired := closeDanglingJSON(partial)
	if err := json.Unmarshal([]byte(repaired), &out); err == nil {
		return out
	}
	return map[string]any{}
}

// closeDanglingJSON appends the minimum set of closing tokens needed to make
// a truncated JSON object/array parseable, dropping any trailing incomplete
// token (unterminated string/number/literal/key).
func closeDanglingJSON(s string) string {
	var stack []byte
	inString := false
	escaped := false
	lastNonSpace := -1

	for i := 0; i < len(s); i++ {
		c := s[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{', '[':
			stack = append(stack, c)
		case '}':
			if len(stack) > 0 && stack[len(stack)-1] == '{' {
				stack = stack[:len(stack)-1]
			}
		case ']':
			if len(stack) > 0 && stack[len(stack)-1] == '[' {
				stack = stack[:len(stack)-1]
			}
		}
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			lastNonSpace = i
		}
	}

	result := s
	if inString {
		// Truncate the unterminated string rather than guessing its content.
		if idx := strings.LastIndexByte(s[:lastNonSpace+1], '"'); idx >= 0 {
			result = s[:idx]
			result = strings.TrimRight(result, " \t\r\n,")
		}
	} else {
		trimmed := strings.TrimRight(strings.TrimSpace(result), ",")
		result = trimmed
	}
	// Drop a trailing dangling key (`"key"` with no `:` and value yet).
	result = strings.TrimRight(result, " \t\r\n")
	if strings.HasSuffix(result, ":") {
		if idx := strings.LastIndexAny(result, "{,"); idx >= 0 {
			result = result[:idx+1]
		}
	}
	for _, s := range slices.Backward(stack) {
		switch s {
		case '{':
			result += "}"
		case '[':
			result += "]"
		}
	}
	return result
}

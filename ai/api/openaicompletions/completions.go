// Package openaicompletions implements streaming against the OpenAI-compatible
// /chat/completions API (SSE, stream=true), used by OpenAI, OpenRouter, and any
// other OpenAI Chat Completions compatible provider. Pragmatic Go port of
// pi-ai's openai-completions.ts.
package openaicompletions

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mikus/maiku/ai"
)

const defaultMaxTokens = 8192

func init() {
	ai.RegisterAPI(ai.APIOpenAICompletions, Stream)
}

// ---- Wire types ----

type chatMessage struct {
	Role       string          `json:"role"`
	Content    any             `json:"content,omitempty"`
	Name       string          `json:"name,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	ToolCalls  []toolCallParam `json:"tool_calls,omitempty"`
}

type toolCallParam struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function functionCall `json:"function"`
}

type functionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type textPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type imageURLPart struct {
	Type     string      `json:"type"`
	ImageURL imageURLRef `json:"image_url"`
}

type imageURLRef struct {
	URL string `json:"url"`
}

type functionTool struct {
	Type     string           `json:"type"`
	Function functionToolBody `json:"function"`
}

type functionToolBody struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type chatRequest struct {
	Model           string          `json:"model"`
	Messages        []chatMessage   `json:"messages"`
	Stream          bool            `json:"stream"`
	StreamOptions   *streamOptions  `json:"stream_options,omitempty"`
	MaxTokens       int             `json:"max_tokens,omitempty"`
	Temperature     *float64        `json:"temperature,omitempty"`
	Tools           []functionTool  `json:"tools,omitempty"`
	ReasoningEffort string          `json:"reasoning_effort,omitempty"`
	ToolChoice      json.RawMessage `json:"tool_choice,omitempty"`
}

type chunkUsage struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	PromptTokensDetails *struct {
		CachedTokens     int `json:"cached_tokens"`
		CacheWriteTokens int `json:"cache_write_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionTokensDetails *struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

type chunkToolCallDelta struct {
	Index    *int   `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function *struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type chunkDelta struct {
	Content          *string              `json:"content"`
	Reasoning        string               `json:"reasoning"`
	ReasoningContent string               `json:"reasoning_content"`
	ReasoningText    string               `json:"reasoning_text"`
	ToolCalls        []chunkToolCallDelta `json:"tool_calls"`
}

type chunkChoice struct {
	Delta        chunkDelta `json:"delta"`
	FinishReason *string    `json:"finish_reason"`
}

type chatCompletionChunk struct {
	ID      string        `json:"id"`
	Model   string        `json:"model"`
	Choices []chunkChoice `json:"choices"`
	Usage   *chunkUsage   `json:"usage"`
}

// Stream implements ai.StreamFn for the OpenAI Chat Completions API.
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
		API:        ai.APIOpenAICompletions,
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
	if apiKey == "" {
		apiKey = "unused"
	}

	req := buildRequest(model, ctxData, opts)
	body, err := json.Marshal(req)
	if err != nil {
		fail(fmt.Errorf("encode request: %w", err), false)
		return
	}

	baseURL := strings.TrimRight(model.BaseURL, "/")
	if baseURL == "" {
		fail(fmt.Errorf("model %s has no baseUrl configured", model.ID), false)
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

	httpReq, err := http.NewRequestWithContext(httpCtx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		fail(fmt.Errorf("build http request: %w", err), false)
		return
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("accept", "text/event-stream")
	httpReq.Header.Set("authorization", "Bearer "+apiKey)
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
			fail(fmt.Errorf("request failed: %w", err), false)
		}
		return
	}
	defer resp.Body.Close()

	if opts.OnResponse != nil {
		hdrs := map[string]string{}
		for k := range resp.Header {
			hdrs[k] = resp.Header.Get(k)
		}
		opts.OnResponse(ai.ProviderResponse{Status: resp.StatusCode, Headers: hdrs}, model)
	}

	if resp.StatusCode >= 400 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		fail(fmt.Errorf("provider API error (%d): %s", resp.StatusCode, sanitizeErrorBody(errBody)), false)
		return
	}

	out.Push(ai.AssistantMessageEvent{Type: "start", Partial: &msg})

	var textBlockIndex = -1
	var thinkingBlockIndex = -1
	toolBlocksByIndex := map[int]*toolCallBlockState{}
	toolBlocksByID := map[string]*toolCallBlockState{}
	hasFinishReason := false

	ensureTextBlock := func() int {
		if textBlockIndex == -1 {
			msg.Content = append(msg.Content, ai.TextBlock(""))
			textBlockIndex = len(msg.Content) - 1
			out.Push(ai.AssistantMessageEvent{Type: "text_start", ContentIndex: textBlockIndex, Partial: &msg})
		}
		return textBlockIndex
	}
	ensureThinkingBlock := func() int {
		if thinkingBlockIndex == -1 {
			msg.Content = append(msg.Content, ai.ThinkingBlock(""))
			thinkingBlockIndex = len(msg.Content) - 1
			out.Push(ai.AssistantMessageEvent{Type: "thinking_start", ContentIndex: thinkingBlockIndex, Partial: &msg})
		}
		return thinkingBlockIndex
	}
	ensureToolCallBlock := func(delta chunkToolCallDelta) *toolCallBlockState {
		var block *toolCallBlockState
		if delta.Index != nil {
			block = toolBlocksByIndex[*delta.Index]
		}
		if block == nil && delta.ID != "" {
			block = toolBlocksByID[delta.ID]
		}
		if block == nil {
			name := ""
			if delta.Function != nil {
				name = delta.Function.Name
			}
			msg.Content = append(msg.Content, ai.AssistantContentBlock{Type: "toolCall", ID: delta.ID, Name: name, Arguments: map[string]any{}})
			block = &toolCallBlockState{contentIndex: len(msg.Content) - 1}
			if delta.Index != nil {
				toolBlocksByIndex[*delta.Index] = block
			}
			if delta.ID != "" {
				toolBlocksByID[delta.ID] = block
			}
			out.Push(ai.AssistantMessageEvent{Type: "toolcall_start", ContentIndex: block.contentIndex, Partial: &msg})
		}
		if delta.ID != "" {
			if msg.Content[block.contentIndex].ID == "" {
				msg.Content[block.contentIndex].ID = delta.ID
			}
			toolBlocksByID[delta.ID] = block
		}
		if delta.Function != nil && delta.Function.Name != "" && msg.Content[block.contentIndex].Name == "" {
			msg.Content[block.contentIndex].Name = delta.Function.Name
		}
		return block
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
		if value == "[DONE]" {
			break
		}

		var chunk chatCompletionChunk
		if err := json.Unmarshal([]byte(value), &chunk); err != nil {
			continue // ignore malformed keep-alive / non-JSON lines
		}

		if msg.ResponseID == "" {
			msg.ResponseID = chunk.ID
		}
		if chunk.Model != "" && chunk.Model != model.ID && msg.ResponseModel == "" {
			msg.ResponseModel = chunk.Model
		}
		if chunk.Usage != nil {
			msg.Usage = parseUsage(*chunk.Usage, model)
		}

		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]

		if choice.FinishReason != nil && *choice.FinishReason != "" {
			msg.RawStopReason = *choice.FinishReason
			stopReason, errMessage := mapStopReason(*choice.FinishReason)
			msg.StopReason = stopReason
			if errMessage != "" {
				msg.ErrorMessage = errMessage
			}
			hasFinishReason = true
		}

		if choice.Delta.Content != nil && *choice.Delta.Content != "" {
			idx := ensureTextBlock()
			block := &msg.Content[idx]
			block.Text += *choice.Delta.Content
			out.Push(ai.AssistantMessageEvent{Type: "text_delta", ContentIndex: idx, Delta: *choice.Delta.Content, Partial: &msg})
		}

		reasoningDelta := firstNonEmpty(choice.Delta.ReasoningContent, choice.Delta.Reasoning, choice.Delta.ReasoningText)
		if reasoningDelta != "" {
			idx := ensureThinkingBlock()
			block := &msg.Content[idx]
			block.Thinking += reasoningDelta
			out.Push(ai.AssistantMessageEvent{Type: "thinking_delta", ContentIndex: idx, Delta: reasoningDelta, Partial: &msg})
		}

		for _, tc := range choice.Delta.ToolCalls {
			block := ensureToolCallBlock(tc)
			delta := ""
			if tc.Function != nil && tc.Function.Arguments != "" {
				delta = tc.Function.Arguments
				block.partialArgs += tc.Function.Arguments
				msg.Content[block.contentIndex].Arguments = parseStreamingJSON(block.partialArgs)
			}
			out.Push(ai.AssistantMessageEvent{Type: "toolcall_delta", ContentIndex: block.contentIndex, Delta: delta, Partial: &msg})
		}
	}
	if err := scanner.Err(); err != nil {
		scanErr = err
	}

	if textBlockIndex != -1 {
		out.Push(ai.AssistantMessageEvent{Type: "text_end", ContentIndex: textBlockIndex, Content: msg.Content[textBlockIndex].Text, Partial: &msg})
	}
	if thinkingBlockIndex != -1 {
		out.Push(ai.AssistantMessageEvent{Type: "thinking_end", ContentIndex: thinkingBlockIndex, Content: msg.Content[thinkingBlockIndex].Thinking, Partial: &msg})
	}
	for idx, block := range toolBlocksByIndex {
		_ = idx
		msg.Content[block.contentIndex].Arguments = parseStreamingJSON(block.partialArgs)
		tc, _ := msg.Content[block.contentIndex].AsToolCall()
		out.Push(ai.AssistantMessageEvent{Type: "toolcall_end", ContentIndex: block.contentIndex, ToolCall: &tc, Partial: &msg})
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
		hasToolCall := false
		for _, b := range msg.Content {
			if b.Type == "toolCall" {
				hasToolCall = true
				break
			}
		}
		if hasToolCall {
			msg.StopReason = ai.StopToolUse
		} else {
			msg.StopReason = ai.StopStop
		}
	}
	if msg.StopReason == ai.StopError {
		errMessage := msg.ErrorMessage
		if errMessage == "" {
			errMessage = "provider returned an error stop reason"
		}
		fail(fmt.Errorf("%s", errMessage), false)
		return
	}
	if msg.StopReason == ai.StopPending {
		fail(fmt.Errorf("stream ended without finish_reason"), false)
		return
	}

	out.Push(ai.AssistantMessageEvent{Type: "done", Reason: msg.StopReason, Message: &msg})
}

type toolCallBlockState struct {
	contentIndex int
	partialArgs  string
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func hasAuthHeader(headers map[string]*string) bool {
	for k, v := range headers {
		lk := strings.ToLower(k)
		if lk == "authorization" && v != nil && strings.TrimSpace(*v) != "" {
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

func buildRequest(model ai.Model, ctxData ai.Context, opts *ai.SimpleStreamOptions) *chatRequest {
	req := &chatRequest{
		Model:         model.ID,
		Messages:      convertMessages(model, ctxData),
		Stream:        true,
		StreamOptions: &streamOptions{IncludeUsage: true},
	}

	maxTokens := model.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}
	if opts.MaxTokens != nil && *opts.MaxTokens > 0 {
		maxTokens = *opts.MaxTokens
	}
	req.MaxTokens = maxTokens

	if opts.Temperature != nil {
		req.Temperature = opts.Temperature
	}

	if len(ctxData.Tools) > 0 {
		tools := make([]functionTool, 0, len(ctxData.Tools))
		for _, t := range ctxData.Tools {
			params := t.Parameters
			if len(params) == 0 {
				params = json.RawMessage(`{"type":"object","properties":{}}`)
			}
			tools = append(tools, functionTool{
				Type: "function",
				Function: functionToolBody{
					Name:        t.Name,
					Description: t.Description,
					Parameters:  params,
				},
			})
		}
		req.Tools = tools
	}

	if model.Reasoning && opts.Reasoning != "" && opts.Reasoning != ai.ThinkingOff {
		req.ReasoningEffort = mapReasoningEffort(opts.Reasoning)
	}

	return req
}

func mapReasoningEffort(level ai.ThinkingLevel) string {
	switch level {
	case ai.ThinkingMinimal:
		return "minimal"
	case ai.ThinkingLow:
		return "low"
	case ai.ThinkingMedium:
		return "medium"
	case ai.ThinkingHigh, ai.ThinkingXHigh, ai.ThinkingMax:
		return "high"
	default:
		return "medium"
	}
}

func convertMessages(model ai.Model, ctxData ai.Context) []chatMessage {
	var out []chatMessage

	if ctxData.SystemPrompt != "" {
		out = append(out, chatMessage{Role: "system", Content: ctxData.SystemPrompt})
	}

	messages := ctxData.Messages
	for i := 0; i < len(messages); i++ {
		m := messages[i]
		switch m.Role {
		case "user":
			out = append(out, convertUserMessage(m.UserContent))
		case "assistant":
			out = append(out, convertAssistantMessage(m))
		case "toolResult":
			j := i
			for j < len(messages) && messages[j].Role == "toolResult" {
				out = append(out, convertToolResultMessage(messages[j]))
				j++
			}
			i = j - 1
		}
	}
	return out
}

func convertUserMessage(content any) chatMessage {
	switch v := content.(type) {
	case string:
		return chatMessage{Role: "user", Content: v}
	case []ai.TextContent:
		var parts []any
		for _, t := range v {
			parts = append(parts, textPart{Type: "text", Text: t.Text})
		}
		return chatMessage{Role: "user", Content: parts}
	case []any:
		var parts []any
		for _, item := range v {
			switch item := item.(type) {
			case string:
				parts = append(parts, textPart{Type: "text", Text: item})
			case ai.TextContent:
				parts = append(parts, textPart{Type: "text", Text: item.Text})
			case ai.ImageContent:
				parts = append(parts, imageURLPart{
					Type: "image_url",
					ImageURL: imageURLRef{URL: fmt.Sprintf("data:%s;base64,%s", item.MimeType, item.Data)},
				})
			case map[string]any:
				switch item["type"] {
				case "text":
					text, _ := item["text"].(string)
					parts = append(parts, textPart{Type: "text", Text: text})
				case "image":
					data, _ := item["data"].(string)
					mimeType, _ := item["mimeType"].(string)
					parts = append(parts, imageURLPart{Type: "image_url", ImageURL: imageURLRef{URL: fmt.Sprintf("data:%s;base64,%s", mimeType, data)}})
				}
			}
		}
		if len(parts) == 0 {
			return chatMessage{Role: "user", Content: ""}
		}
		return chatMessage{Role: "user", Content: parts}
	default:
		return chatMessage{Role: "user", Content: ""}
	}
}

func convertAssistantMessage(m ai.Message) chatMessage {
	msg := chatMessage{Role: "assistant"}
	var textParts []string
	var toolCalls []toolCallParam

	for _, b := range m.AssistantContent {
		switch b.Type {
		case "text":
			if strings.TrimSpace(b.Text) != "" {
				textParts = append(textParts, b.Text)
			}
		case "toolCall":
			args := b.Arguments
			if args == nil {
				args = map[string]any{}
			}
			argsJSON, _ := json.Marshal(args)
			toolCalls = append(toolCalls, toolCallParam{
				ID:   b.ID,
				Type: "function",
				Function: functionCall{
					Name:      b.Name,
					Arguments: string(argsJSON),
				},
			})
		}
	}
	if len(textParts) > 0 {
		msg.Content = strings.Join(textParts, "")
	}
	msg.ToolCalls = toolCalls
	return msg
}

func convertToolResultMessage(m ai.Message) chatMessage {
	var texts []string
	hasImage := false
	for _, c := range m.ToolContent {
		if c.Type == "image" {
			hasImage = true
			continue
		}
		if c.Text != "" {
			texts = append(texts, c.Text)
		}
	}
	content := strings.Join(texts, "\n")
	if content == "" {
		if hasImage {
			content = "(see attached image)"
		} else {
			content = "(no tool output)"
		}
	}
	return chatMessage{Role: "tool", Content: content, ToolCallID: m.ToolCallID}
}

func parseUsage(u chunkUsage, model ai.Model) ai.Usage {
	cacheRead := 0
	cacheWrite := 0
	if u.PromptTokensDetails != nil {
		cacheRead = u.PromptTokensDetails.CachedTokens
		cacheWrite = u.PromptTokensDetails.CacheWriteTokens
	}
	input := u.PromptTokens - cacheRead - cacheWrite
	if input < 0 {
		input = 0
	}
	usage := ai.Usage{
		Input:      input,
		Output:     u.CompletionTokens,
		CacheRead:  cacheRead,
		CacheWrite: cacheWrite,
	}
	if u.CompletionTokensDetails != nil && u.CompletionTokensDetails.ReasoningTokens > 0 {
		reasoning := u.CompletionTokensDetails.ReasoningTokens
		usage.Reasoning = &reasoning
	}
	usage.TotalTokens = usage.Input + usage.Output + usage.CacheRead + usage.CacheWrite
	usage.Cost = ai.CalculateCost(model.Cost, usage)
	return usage
}

func mapStopReason(reason string) (ai.StopReason, string) {
	switch reason {
	case "stop", "end":
		return ai.StopStop, ""
	case "length":
		return ai.StopLength, ""
	case "function_call", "tool_calls":
		return ai.StopToolUse, ""
	case "content_filter":
		return ai.StopError, "provider finish_reason: content_filter"
	case "network_error":
		return ai.StopError, "provider finish_reason: network_error"
	default:
		return ai.StopError, fmt.Sprintf("provider finish_reason: %s", reason)
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

func closeDanglingJSON(s string) string {
	var stack []byte
	inString := false
	escaped := false
	lastNonSpace := -1

	for i := 0; i < len(s); i++ {
		c := s[i]
		if inString {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
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
		if idx := strings.LastIndexByte(s[:lastNonSpace+1], '"'); idx >= 0 {
			result = s[:idx]
			result = strings.TrimRight(result, " \t\r\n,")
		}
	} else {
		result = strings.TrimRight(strings.TrimSpace(result), ",")
	}
	result = strings.TrimRight(result, " \t\r\n")
	if strings.HasSuffix(result, ":") {
		if idx := strings.LastIndexAny(result, "{,"); idx >= 0 {
			result = result[:idx+1]
		}
	}
	for i := len(stack) - 1; i >= 0; i-- {
		switch stack[i] {
		case '{':
			result += "}"
		case '[':
			result += "]"
		}
	}
	return result
}

// Package openaicodex implements streaming against ChatGPT Codex
// (chatgpt.com/backend-api/codex/responses). SSE-only port of pi-ai's
// openai-codex-responses.ts (websocket transport not ported).
package openaicodex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/mikus/maiku/ai"
)

const (
	defaultBaseURL   = "https://chatgpt.com/backend-api"
	defaultMaxTokens = 8192
	jwtClaimPath     = "https://api.openai.com/auth"
)

func init() {
	ai.RegisterAPI(ai.APIOpenAICodexResponses, Stream)
}

// ---- Wire types: request ----

type responsesTool struct {
	Type        string          `json:"type"` // "function"
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

type reasoningParam struct {
	Effort  string `json:"effort,omitempty"`
	Summary string `json:"summary,omitempty"`
}

type textParam struct {
	Verbosity string `json:"verbosity,omitempty"`
}

type responsesRequest struct {
	Model              string          `json:"model"`
	Instructions       string          `json:"instructions,omitempty"`
	Input              []any           `json:"input"`
	Stream             bool            `json:"stream"`
	Store              bool            `json:"store"`
	Tools              []responsesTool `json:"tools,omitempty"`
	ToolChoice         string          `json:"tool_choice,omitempty"`
	ParallelToolCalls  bool            `json:"parallel_tool_calls,omitempty"`
	Temperature        *float64        `json:"temperature,omitempty"`
	Reasoning          *reasoningParam `json:"reasoning,omitempty"`
	Text               *textParam      `json:"text,omitempty"`
	Include            []string        `json:"include,omitempty"`
	PromptCacheKey     string          `json:"prompt_cache_key,omitempty"`
}

// Input item shapes (marshaled as elements of responsesRequest.Input).

type roleMessageItem struct {
	Role    string `json:"role"` // "system" | "developer" | "user"
	Content any    `json:"content"`
}

type inputTextPart struct {
	Type string `json:"type"` // "input_text"
	Text string `json:"text"`
}

type inputImagePart struct {
	Type     string `json:"type"` // "input_image"
	Detail   string `json:"detail"`
	ImageURL string `json:"image_url"`
}

type outputTextPart struct {
	Type string `json:"type"` // "output_text"
	Text string `json:"text"`
}

type assistantMessageItem struct {
	Type    string           `json:"type"` // "message"
	Role    string           `json:"role"` // "assistant"
	Content []outputTextPart `json:"content"`
	Status  string           `json:"status"`
	ID      string           `json:"id,omitempty"`
}

type functionCallItem struct {
	Type      string `json:"type"` // "function_call"
	ID        string `json:"id,omitempty"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type functionCallOutputItem struct {
	Type   string `json:"type"` // "function_call_output"
	CallID string `json:"call_id"`
	Output any    `json:"output"`
}

// ---- Wire types: streamed events ----

type responseItem struct {
	Type             string `json:"type"`
	ID               string `json:"id"`
	CallID           string `json:"call_id"`
	Name             string `json:"name"`
	Arguments        string `json:"arguments"`
	Input            string `json:"input"`
	Status           string `json:"status"`
	Phase            string `json:"phase"`
	EncryptedContent string `json:"encrypted_content"`
	Summary          []struct {
		Text string `json:"text"`
	} `json:"summary"`
	Content []struct {
		Type    string `json:"type"`
		Text    string `json:"text"`
		Refusal string `json:"refusal"`
	} `json:"content"`
}

type responseUsage struct {
	InputTokens        int `json:"input_tokens"`
	OutputTokens       int `json:"output_tokens"`
	TotalTokens        int `json:"total_tokens"`
	InputTokensDetails *struct {
		CachedTokens     int `json:"cached_tokens"`
		CacheWriteTokens int `json:"cache_write_tokens"`
	} `json:"input_tokens_details"`
	OutputTokensDetails *struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`
}

type responseObject struct {
	ID                string         `json:"id"`
	Status            string         `json:"status"`
	Usage             *responseUsage `json:"usage"`
	IncompleteDetails *struct {
		Reason string `json:"reason"`
	} `json:"incomplete_details"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type sseEvent struct {
	Type        string          `json:"type"`
	OutputIndex *int            `json:"output_index"`
	Item        json.RawMessage `json:"item"`
	Delta       string          `json:"delta"`
	Arguments   string          `json:"arguments"`
	Input       string          `json:"input"`
	Response    *responseObject `json:"response"`
	Code        string          `json:"code"`
	Message     string          `json:"message"`
}

// Stream implements ai.StreamFn for the OpenAI Responses API.
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
		API:        ai.APIOpenAICodexResponses,
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
		baseURL = defaultBaseURL
	}
	url := resolveCodexURL(baseURL)

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

	accountID, err := extractAccountID(apiKey)
	if err != nil {
		fail(err, false)
		return
	}

	httpReq, err := http.NewRequestWithContext(httpCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		fail(fmt.Errorf("build http request: %w", err), false)
		return
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("accept", "text/event-stream")
	httpReq.Header.Set("authorization", "Bearer "+apiKey)
	httpReq.Header.Set("chatgpt-account-id", accountID)
	httpReq.Header.Set("openai-beta", "responses=experimental")
	httpReq.Header.Set("originator", "maiku")
	httpReq.Header.Set("user-agent", fmt.Sprintf("maiku (%s %s; %s)", runtime.GOOS, runtimeVersion(), runtime.GOARCH))
	if opts.SessionID != "" {
		httpReq.Header.Set("session-id", opts.SessionID)
		httpReq.Header.Set("x-client-request-id", opts.SessionID)
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
			fail(fmt.Errorf("codex request failed: %w", err), false)
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
		fail(fmt.Errorf("codex API error (%d): %s", resp.StatusCode, sanitizeCodexError(errBody, resp.StatusCode)), false)
		return
	}

	out.Push(ai.AssistantMessageEvent{Type: "start", Partial: &msg})

	sawTerminal := false
	slots := map[int]*outputSlot{}

	handleErr := processEvent(out, &msg, model, slots, &sawTerminal)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	var streamErr error
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

		var ev sseEvent
		if err := json.Unmarshal([]byte(value), &ev); err != nil {
			continue // ignore malformed keep-alive / non-JSON lines
		}
		if err := handleErr(ev); err != nil {
			streamErr = err
			break
		}
	}
	if streamErr == nil {
		if err := scanner.Err(); err != nil {
			streamErr = err
		}
	}

	if streamErr != nil {
		if aborted {
			fail(fmt.Errorf("request was aborted"), true)
		} else {
			fail(streamErr, false)
		}
		return
	}
	if aborted {
		fail(fmt.Errorf("request was aborted"), true)
		return
	}
	if !sawTerminal {
		fail(fmt.Errorf("openai responses stream ended before a terminal response event"), false)
		return
	}
	if msg.StopReason == ai.StopPending {
		fail(fmt.Errorf("openai responses stream ended without a stop reason"), false)
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

// ---- Streaming event handling ----

type outputSlot struct {
	kind         string // "thinking" | "text" | "toolCall"
	contentIndex int
	partialJSON  string // toolCall only
}

// processEvent returns a closure over per-stream state that applies one SSE
// event to msg/out, mirroring pi-ai's processResponsesStream.
func processEvent(out *ai.AssistantMessageEventStream, msg *ai.AssistantMessage, model ai.Model, slots map[int]*outputSlot, sawTerminal *bool) func(ev sseEvent) error {
	createSlot := func(outputIndex int, item responseItem, rawItem json.RawMessage) *outputSlot {
		switch item.Type {
		case "reasoning":
			msg.Content = append(msg.Content, ai.ThinkingBlock(""))
			slot := &outputSlot{kind: "thinking", contentIndex: len(msg.Content) - 1}
			slots[outputIndex] = slot
			out.Push(ai.AssistantMessageEvent{Type: "thinking_start", ContentIndex: slot.contentIndex, Partial: msg})
			return slot
		case "message":
			if item.Phase == "final_answer" {
				msg.StopReason = ai.StopStop
			}
			msg.Content = append(msg.Content, ai.TextBlock(""))
			slot := &outputSlot{kind: "text", contentIndex: len(msg.Content) - 1}
			slots[outputIndex] = slot
			out.Push(ai.AssistantMessageEvent{Type: "text_start", ContentIndex: slot.contentIndex, Partial: msg})
			return slot
		case "function_call":
			id := item.CallID
			if item.ID != "" {
				id = item.CallID + "|" + item.ID
			}
			msg.Content = append(msg.Content, ai.AssistantContentBlock{Type: "toolCall", ID: id, Name: item.Name, Arguments: map[string]any{}})
			slot := &outputSlot{kind: "toolCall", contentIndex: len(msg.Content) - 1, partialJSON: item.Arguments}
			slots[outputIndex] = slot
			out.Push(ai.AssistantMessageEvent{Type: "toolcall_start", ContentIndex: slot.contentIndex, Partial: msg})
			return slot
		default:
			return nil // custom_tool_call / tool_search / other item kinds are not ported.
		}
	}

	getOrCreateSlot := func(outputIndex int, rawItem json.RawMessage) *outputSlot {
		if slot, ok := slots[outputIndex]; ok {
			return slot
		}
		var item responseItem
		if err := json.Unmarshal(rawItem, &item); err != nil {
			return nil
		}
		return createSlot(outputIndex, item, rawItem)
	}

	return func(ev sseEvent) error {
		switch ev.Type {
		case "response.created":
			if ev.Response != nil && ev.Response.ID != "" {
				msg.ResponseID = ev.Response.ID
			}

		case "response.output_item.added":
			if ev.OutputIndex == nil || len(ev.Item) == 0 {
				return nil
			}
			var item responseItem
			if err := json.Unmarshal(ev.Item, &item); err != nil {
				return nil
			}
			createSlot(*ev.OutputIndex, item, ev.Item)

		case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
			if ev.OutputIndex == nil {
				return nil
			}
			slot := slots[*ev.OutputIndex]
			if slot == nil || slot.kind != "thinking" {
				return nil
			}
			msg.Content[slot.contentIndex].Thinking += ev.Delta
			out.Push(ai.AssistantMessageEvent{Type: "thinking_delta", ContentIndex: slot.contentIndex, Delta: ev.Delta, Partial: msg})

		case "response.reasoning_summary_part.done":
			if ev.OutputIndex == nil {
				return nil
			}
			slot := slots[*ev.OutputIndex]
			if slot == nil || slot.kind != "thinking" {
				return nil
			}
			msg.Content[slot.contentIndex].Thinking += "\n\n"
			out.Push(ai.AssistantMessageEvent{Type: "thinking_delta", ContentIndex: slot.contentIndex, Delta: "\n\n", Partial: msg})

		case "response.output_text.delta", "response.refusal.delta":
			if ev.OutputIndex == nil {
				return nil
			}
			slot := slots[*ev.OutputIndex]
			if slot == nil || slot.kind != "text" {
				return nil
			}
			msg.Content[slot.contentIndex].Text += ev.Delta
			out.Push(ai.AssistantMessageEvent{Type: "text_delta", ContentIndex: slot.contentIndex, Delta: ev.Delta, Partial: msg})

		case "response.function_call_arguments.delta":
			if ev.OutputIndex == nil {
				return nil
			}
			slot := slots[*ev.OutputIndex]
			if slot == nil || slot.kind != "toolCall" {
				return nil
			}
			slot.partialJSON += ev.Delta
			msg.Content[slot.contentIndex].Arguments = parseStreamingJSON(slot.partialJSON)
			out.Push(ai.AssistantMessageEvent{Type: "toolcall_delta", ContentIndex: slot.contentIndex, Delta: ev.Delta, Partial: msg})

		case "response.function_call_arguments.done":
			if ev.OutputIndex == nil {
				return nil
			}
			slot := slots[*ev.OutputIndex]
			if slot == nil || slot.kind != "toolCall" {
				return nil
			}
			previous := slot.partialJSON
			slot.partialJSON = ev.Arguments
			msg.Content[slot.contentIndex].Arguments = parseStreamingJSON(slot.partialJSON)
			if strings.HasPrefix(ev.Arguments, previous) {
				delta := ev.Arguments[len(previous):]
				if delta != "" {
					out.Push(ai.AssistantMessageEvent{Type: "toolcall_delta", ContentIndex: slot.contentIndex, Delta: delta, Partial: msg})
				}
			}

		case "response.output_item.done":
			if ev.OutputIndex == nil || len(ev.Item) == 0 {
				return nil
			}
			var item responseItem
			if err := json.Unmarshal(ev.Item, &item); err != nil {
				return nil
			}
			if item.Phase == "final_answer" {
				msg.StopReason = ai.StopStop
			}
			slot := getOrCreateSlot(*ev.OutputIndex, ev.Item)
			if slot == nil {
				return nil
			}
			switch {
			case item.Type == "reasoning" && slot.kind == "thinking":
				summary := joinTexts(item.Summary)
				content := joinContentTexts(item.Content)
				thinking := summary
				if thinking == "" {
					thinking = content
				}
				if thinking == "" {
					thinking = msg.Content[slot.contentIndex].Thinking
				}
				msg.Content[slot.contentIndex].Thinking = thinking
				msg.Content[slot.contentIndex].ThinkingSignature = string(ev.Item)
				out.Push(ai.AssistantMessageEvent{Type: "thinking_end", ContentIndex: slot.contentIndex, Content: thinking, Partial: msg})
				delete(slots, *ev.OutputIndex)
			case item.Type == "message" && slot.kind == "text":
				text := joinContentTexts(item.Content)
				msg.Content[slot.contentIndex].Text = text
				msg.Content[slot.contentIndex].TextSignature = encodeTextSignature(item.ID, item.Phase)
				out.Push(ai.AssistantMessageEvent{Type: "text_end", ContentIndex: slot.contentIndex, Content: text, Partial: msg})
				delete(slots, *ev.OutputIndex)
			case item.Type == "function_call" && slot.kind == "toolCall":
				args := item.Arguments
				if args == "" {
					args = slot.partialJSON
				}
				if args == "" {
					args = "{}"
				}
				msg.Content[slot.contentIndex].Arguments = parseStreamingJSON(args)
				tc, _ := msg.Content[slot.contentIndex].AsToolCall()
				out.Push(ai.AssistantMessageEvent{Type: "toolcall_end", ContentIndex: slot.contentIndex, ToolCall: &tc, Partial: msg})
				delete(slots, *ev.OutputIndex)
			}

		case "response.completed", "response.incomplete":
			*sawTerminal = true
			finalizeResponse(msg, model, ev.Response)

		case "error":
			return fmt.Errorf("error code %s: %s", ev.Code, ev.Message)

		case "response.failed":
			*sawTerminal = true
			if ev.Response != nil {
				msg.RawStopReason = ev.Response.Status
				if ev.Response.Error != nil {
					return fmt.Errorf("%s: %s", firstNonEmpty(ev.Response.Error.Code, "unknown"), firstNonEmpty(ev.Response.Error.Message, "no message"))
				}
				if ev.Response.IncompleteDetails != nil && ev.Response.IncompleteDetails.Reason != "" {
					return fmt.Errorf("incomplete: %s", ev.Response.IncompleteDetails.Reason)
				}
			}
			return fmt.Errorf("unknown error (no error details in response)")
		}
		return nil
	}
}

func finalizeResponse(msg *ai.AssistantMessage, model ai.Model, resp *responseObject) {
	if resp == nil {
		msg.StopReason = ai.StopStop
		return
	}
	if resp.ID != "" {
		msg.ResponseID = resp.ID
	}
	if resp.Usage != nil {
		cacheRead, cacheWrite := 0, 0
		if resp.Usage.InputTokensDetails != nil {
			cacheRead = resp.Usage.InputTokensDetails.CachedTokens
			cacheWrite = resp.Usage.InputTokensDetails.CacheWriteTokens
		}
		input := resp.Usage.InputTokens - cacheRead - cacheWrite
		if input < 0 {
			input = 0
		}
		usage := ai.Usage{
			Input:       input,
			Output:      resp.Usage.OutputTokens,
			CacheRead:   cacheRead,
			CacheWrite:  cacheWrite,
			TotalTokens: resp.Usage.TotalTokens,
		}
		if resp.Usage.OutputTokensDetails != nil && resp.Usage.OutputTokensDetails.ReasoningTokens > 0 {
			r := resp.Usage.OutputTokensDetails.ReasoningTokens
			usage.Reasoning = &r
		}
		usage.Cost = ai.CalculateCost(model.Cost, usage)
		msg.Usage = usage
	}

	incompleteReason := ""
	if resp.IncompleteDetails != nil {
		incompleteReason = resp.IncompleteDetails.Reason
	}
	if incompleteReason != "" {
		msg.RawStopReason = resp.Status + "." + incompleteReason
	} else {
		msg.RawStopReason = resp.Status
	}
	stopReason, errMessage := mapStopReason(resp.Status, incompleteReason)
	msg.StopReason = stopReason
	if errMessage != "" {
		msg.ErrorMessage = errMessage
	}
	for _, b := range msg.Content {
		if b.Type == "toolCall" {
			if msg.StopReason == ai.StopStop {
				msg.StopReason = ai.StopToolUse
			}
			break
		}
	}
}

func mapStopReason(status, incompleteReason string) (ai.StopReason, string) {
	switch status {
	case "", "completed", "in_progress", "queued":
		return ai.StopStop, ""
	case "incomplete":
		if incompleteReason == "max_output_tokens" {
			return ai.StopLength, ""
		}
		if incompleteReason != "" {
			return ai.StopError, fmt.Sprintf("response incomplete: %s", incompleteReason)
		}
		return ai.StopError, "response incomplete without a provider reason"
	case "failed", "cancelled":
		return ai.StopError, ""
	default:
		return ai.StopError, fmt.Sprintf("unhandled response status: %s", status)
	}
}

func joinTexts(items []struct {
	Text string `json:"text"`
}) string {
	parts := make([]string, 0, len(items))
	for _, it := range items {
		parts = append(parts, it.Text)
	}
	return strings.Join(parts, "\n\n")
}

func joinContentTexts(items []struct {
	Type    string `json:"type"`
	Text    string `json:"text"`
	Refusal string `json:"refusal"`
}) string {
	parts := make([]string, 0, len(items))
	for _, it := range items {
		if it.Type == "output_text" {
			parts = append(parts, it.Text)
		} else {
			parts = append(parts, it.Refusal)
		}
	}
	return strings.Join(parts, "")
}

func encodeTextSignature(id, phase string) string {
	if id == "" {
		return ""
	}
	if phase == "commentary" || phase == "final_answer" {
		b, _ := json.Marshal(map[string]any{"v": 1, "id": id, "phase": phase})
		return string(b)
	}
	b, _ := json.Marshal(map[string]any{"v": 1, "id": id})
	return string(b)
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

func resolveCodexURL(baseURL string) string {
	normalized := strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(normalized, "/codex/responses") {
		return normalized
	}
	if strings.HasSuffix(normalized, "/codex") {
		return normalized + "/responses"
	}
	return normalized + "/codex/responses"
}

func extractAccountID(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("openai-codex requires a ChatGPT OAuth access token (JWT)")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		// Some tokens use standard base64 with padding.
		payload, err = base64.StdEncoding.DecodeString(padBase64(parts[1]))
		if err != nil {
			return "", fmt.Errorf("failed to decode openai-codex token: %w", err)
		}
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", fmt.Errorf("failed to parse openai-codex token: %w", err)
	}
	authObj, _ := claims[jwtClaimPath].(map[string]any)
	accountID, _ := authObj["chatgpt_account_id"].(string)
	if accountID == "" {
		return "", fmt.Errorf("openai-codex token missing chatgpt_account_id — sign in with ChatGPT Plus/Pro")
	}
	return accountID, nil
}

func padBase64(s string) string {
	switch len(s) % 4 {
	case 2:
		return s + "=="
	case 3:
		return s + "="
	default:
		return s
	}
}

func sanitizeCodexError(body []byte, status int) string {
	raw := sanitizeErrorBody(body)
	var parsed struct {
		Error *struct {
			Code     string `json:"code"`
			Type     string `json:"type"`
			Message  string `json:"message"`
			PlanType string `json:"plan_type"`
			ResetsAt int64  `json:"resets_at"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil || parsed.Error == nil {
		return raw
	}
	err := parsed.Error
	code := err.Code
	if code == "" {
		code = err.Type
	}
	if status == 429 || strings.Contains(strings.ToLower(code), "usage_limit") ||
		strings.Contains(strings.ToLower(code), "rate_limit") {
		plan := ""
		if err.PlanType != "" {
			plan = " (" + strings.ToLower(err.PlanType) + " plan)"
		}
		when := ""
		if err.ResetsAt > 0 {
			mins := (err.ResetsAt*1000 - time.Now().UnixMilli()) / 60000
			if mins < 0 {
				mins = 0
			}
			when = fmt.Sprintf(" Try again in ~%d min.", mins)
		}
		return fmt.Sprintf("You have hit your ChatGPT usage limit%s.%s", plan, when)
	}
	if err.Message != "" {
		return err.Message
	}
	return raw
}

func runtimeVersion() string {
	return runtime.GOOS
}

// ---- Request building ----

func buildRequest(model ai.Model, ctxData ai.Context, opts *ai.SimpleStreamOptions) *responsesRequest {
	instructions := strings.TrimSpace(ctxData.SystemPrompt)
	if instructions == "" {
		instructions = "You are a helpful assistant."
	}

	req := &responsesRequest{
		Model:             model.ID,
		Instructions:      instructions,
		Input:             convertMessages(model, ctxData),
		Stream:            true,
		Store:             false,
		Text:              &textParam{Verbosity: "low"},
		Include:           []string{"reasoning.encrypted_content"},
		ToolChoice:        "auto",
		ParallelToolCalls: true,
	}
	if opts.SessionID != "" && opts.CacheRetention != ai.CacheNone {
		req.PromptCacheKey = opts.SessionID
	}

	if opts.Temperature != nil {
		req.Temperature = opts.Temperature
	}

	if len(ctxData.Tools) > 0 {
		tools := make([]responsesTool, 0, len(ctxData.Tools))
		for _, t := range ctxData.Tools {
			params := t.Parameters
			if len(params) == 0 {
				params = json.RawMessage(`{"type":"object","properties":{}}`)
			}
			tools = append(tools, responsesTool{
				Type:        "function",
				Name:        t.Name,
				Description: t.Description,
				Parameters:  params,
			})
		}
		req.Tools = tools
	}

	if opts.Reasoning != "" && opts.Reasoning != ai.ThinkingOff {
		req.Reasoning = &reasoningParam{
			Effort:  mapReasoningEffort(model, opts.Reasoning),
			Summary: "auto",
		}
	}

	return req
}

func mapReasoningEffort(model ai.Model, level ai.ThinkingLevel) string {
	if override, ok := model.ThinkingLevelMap[level]; ok && override != nil {
		return *override
	}
	effort := string(level)
	switch level {
	case ai.ThinkingOff:
		return "none"
	case ai.ThinkingMinimal:
		effort = "minimal"
	case ai.ThinkingLow:
		effort = "low"
	case ai.ThinkingMedium:
		effort = "medium"
	case ai.ThinkingHigh:
		effort = "high"
	case ai.ThinkingXHigh, ai.ThinkingMax:
		effort = "xhigh"
	}
	return clampCodexReasoningEffort(model.ID, effort)
}

func clampCodexReasoningEffort(modelID, effort string) string {
	id := strings.ToLower(modelID)
	if (strings.HasPrefix(id, "gpt-5.2") || strings.HasPrefix(id, "gpt-5.3") ||
		strings.HasPrefix(id, "gpt-5.4") || strings.HasPrefix(id, "gpt-5.5")) &&
		effort == "minimal" {
		return "low"
	}
	if id == "gpt-5.1" && effort == "xhigh" {
		return "high"
	}
	if id == "gpt-5.1-codex-mini" {
		if effort == "high" || effort == "xhigh" {
			return "high"
		}
		return "medium"
	}
	return effort
}

func convertMessages(model ai.Model, ctxData ai.Context) []any {
	var out []any
	// System prompt is sent via instructions, not as an input message.
	messages := ctxData.Messages
	msgIndex := 0
	for i := 0; i < len(messages); i++ {
		m := messages[i]
		switch m.Role {
		case "user":
			if item := convertUserMessage(m.UserContent); item != nil {
				out = append(out, item)
			}
		case "assistant":
			out = append(out, convertAssistantMessage(m, msgIndex)...)
		case "toolResult":
			j := i
			for j < len(messages) && messages[j].Role == "toolResult" {
				out = append(out, convertToolResultMessage(model, messages[j]))
				j++
			}
			i = j - 1
		}
		msgIndex++
	}
	return out
}

func convertUserMessage(content any) any {
	switch v := content.(type) {
	case string:
		if v == "" {
			return nil
		}
		return roleMessageItem{Role: "user", Content: []any{inputTextPart{Type: "input_text", Text: v}}}
	case []ai.TextContent:
		var parts []any
		for _, t := range v {
			parts = append(parts, inputTextPart{Type: "input_text", Text: t.Text})
		}
		if len(parts) == 0 {
			return nil
		}
		return roleMessageItem{Role: "user", Content: parts}
	case []any:
		var parts []any
		for _, item := range v {
			switch item := item.(type) {
			case string:
				parts = append(parts, inputTextPart{Type: "input_text", Text: item})
			case ai.TextContent:
				parts = append(parts, inputTextPart{Type: "input_text", Text: item.Text})
			case ai.ImageContent:
				parts = append(parts, inputImagePart{
					Type: "input_image", Detail: "auto",
					ImageURL: fmt.Sprintf("data:%s;base64,%s", item.MimeType, item.Data),
				})
			case map[string]any:
				switch item["type"] {
				case "text":
					text, _ := item["text"].(string)
					parts = append(parts, inputTextPart{Type: "input_text", Text: text})
				case "image":
					data, _ := item["data"].(string)
					mimeType, _ := item["mimeType"].(string)
					parts = append(parts, inputImagePart{Type: "input_image", Detail: "auto", ImageURL: fmt.Sprintf("data:%s;base64,%s", mimeType, data)})
				}
			}
		}
		if len(parts) == 0 {
			return nil
		}
		return roleMessageItem{Role: "user", Content: parts}
	default:
		return nil
	}
}

func convertAssistantMessage(m ai.Message, msgIndex int) []any {
	var out []any
	textBlockIndex := 0
	for _, b := range m.AssistantContent {
		switch b.Type {
		case "thinking":
			if b.Redacted || strings.TrimSpace(b.ThinkingSignature) == "" {
				continue // No signature to replay verbatim; drop rather than guess the wire shape.
			}
			out = append(out, json.RawMessage(b.ThinkingSignature))
		case "text":
			id := parseTextSignatureID(b.TextSignature)
			if id == "" {
				if textBlockIndex == 0 {
					id = fmt.Sprintf("msg_pi_%d", msgIndex)
				} else {
					id = fmt.Sprintf("msg_pi_%d_%d", msgIndex, textBlockIndex)
				}
			}
			textBlockIndex++
			out = append(out, assistantMessageItem{
				Type:    "message",
				Role:    "assistant",
				Content: []outputTextPart{{Type: "output_text", Text: b.Text}},
				Status:  "completed",
				ID:      id,
			})
		case "toolCall":
			callID, itemID := splitToolCallID(b.ID)
			args := b.Arguments
			if args == nil {
				args = map[string]any{}
			}
			argsJSON, _ := json.Marshal(args)
			out = append(out, functionCallItem{
				Type:      "function_call",
				ID:        itemID,
				CallID:    callID,
				Name:      b.Name,
				Arguments: string(argsJSON),
			})
		}
	}
	return out
}

func convertToolResultMessage(model ai.Model, m ai.Message) functionCallOutputItem {
	callID, _ := splitToolCallID(m.ToolCallID)
	return functionCallOutputItem{
		Type:   "function_call_output",
		CallID: callID,
		Output: convertToolResultOutput(model, m.ToolContent),
	}
}

func convertToolResultOutput(model ai.Model, content []ai.ToolResultContent) any {
	var texts []string
	var images []ai.ToolResultContent
	for _, c := range content {
		if c.Type == "image" {
			images = append(images, c)
			continue
		}
		if c.Text != "" {
			texts = append(texts, c.Text)
		}
	}
	text := strings.Join(texts, "\n")
	supportsImages := false
	for _, in := range model.Input {
		if in == "image" {
			supportsImages = true
			break
		}
	}
	if len(images) == 0 || !supportsImages {
		if text != "" {
			return text
		}
		if len(images) > 0 {
			return "(see attached image)"
		}
		return "(no tool output)"
	}

	var parts []any
	if text != "" {
		parts = append(parts, inputTextPart{Type: "input_text", Text: text})
	}
	for _, img := range images {
		parts = append(parts, inputImagePart{Type: "input_image", Detail: "auto", ImageURL: fmt.Sprintf("data:%s;base64,%s", img.MimeType, img.Data)})
	}
	return parts
}

// splitToolCallID splits a "callId|itemId" tool call id into its parts. When
// there is no "|" (e.g. the id came from a different provider/API), the
// whole id is treated as the call id and no item id is replayed.
func splitToolCallID(id string) (callID, itemID string) {
	callID, itemID, ok := strings.Cut(id, "|")
	if !ok {
		return id, ""
	}
	return callID, itemID
}

// parseTextSignatureID extracts the message item id from a text signature
// produced by this API (JSON: {"v":1,"id":...}), or "" if absent/foreign.
func parseTextSignatureID(sig string) string {
	if sig == "" {
		return ""
	}
	if strings.HasPrefix(sig, "{") {
		var parsed struct {
			V  int    `json:"v"`
			ID string `json:"id"`
		}
		if err := json.Unmarshal([]byte(sig), &parsed); err == nil && parsed.V == 1 && parsed.ID != "" {
			return parsed.ID
		}
		return ""
	}
	return sig
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

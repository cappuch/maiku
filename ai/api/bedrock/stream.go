package bedrock

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream"
	"github.com/cappuch/maiku/ai"
	"github.com/cappuch/maiku/ai/auth"
)

func init() { ai.RegisterAPI(ai.APIBedrockConverseStream, Stream) }

func Stream(model ai.Model, ctx ai.Context, opts *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
	out := ai.NewAssistantMessageEventStream()
	go run(out, model, ctx, opts)
	return out
}

func run(out *ai.AssistantMessageEventStream, model ai.Model, data ai.Context, opts *ai.SimpleStreamOptions) {
	if opts == nil {
		opts = &ai.SimpleStreamOptions{}
	}
	msg := ai.AssistantMessage{Role: "assistant", Content: []ai.AssistantContentBlock{}, API: ai.APIBedrockConverseStream, Provider: model.Provider, Model: model.ID, StopReason: ai.StopPending, Timestamp: time.Now().UnixMilli()}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if opts.TimeoutMs != nil {
		var timeoutCancel context.CancelFunc
		ctx, timeoutCancel = context.WithTimeout(ctx, time.Duration(*opts.TimeoutMs)*time.Millisecond)
		defer timeoutCancel()
	}
	if opts.Signal != nil {
		select {
		case <-opts.Signal:
			cancel()
		default:
		}
		go func() {
			select {
			case <-opts.Signal:
				cancel()
			case <-ctx.Done():
			}
		}()
	}
	fail := func(err error) {
		msg.StopReason = ai.StopError
		select {
		case <-opts.Signal:
			msg.StopReason = ai.StopAborted
		default:
		}
		msg.ErrorMessage = err.Error()
		out.Push(ai.AssistantMessageEvent{Type: "error", Reason: msg.StopReason, Error: snapshot(msg)})
	}
	reqData, err := buildRequest(model, data, opts)
	if err != nil {
		fail(err)
		return
	}
	var payload any = reqData
	if opts.OnPayload != nil {
		payload, err = opts.OnPayload(payload, model)
		if err != nil {
			fail(err)
			return
		}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		fail(err)
		return
	}
	cfg, err := loadConfig(ctx, opts)
	if err != nil {
		fail(err)
		return
	}
	base := strings.TrimRight(model.BaseURL, "/")
	if base == "" {
		base = endpoint(cfg, "bedrock-runtime")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/model/"+url.PathEscape(model.ID)+"/converse-stream", bytes.NewReader(body))
	if err != nil {
		fail(err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.amazon.eventstream")
	for k, v := range model.Headers {
		req.Header.Set(k, v)
	}
	for k, v := range opts.Headers {
		if v == nil {
			req.Header.Del(k)
		} else {
			req.Header.Set(k, *v)
		}
	}
	token := opts.APIKey
	if token == "" {
		token = opts.Env["AWS_BEARER_TOKEN_BEDROCK"]
	}
	if token == "" {
		token = auth.ResolveAPIKey(ProviderID)
	}
	resp, err := ai.DoHTTP(client(cfg, token, body), req, ai.HTTPRetryPolicyFromStreamOptions(opts))
	if err != nil {
		fail(err)
		return
	}
	defer resp.Body.Close()
	if opts.OnResponse != nil {
		headers := map[string]string{}
		for k := range resp.Header {
			headers[k] = resp.Header.Get(k)
		}
		opts.OnResponse(ai.ProviderResponse{Status: resp.StatusCode, Headers: headers}, model)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		fail(fmt.Errorf("Bedrock API error (%d): %s", resp.StatusCode, strings.TrimSpace(string(body))))
		return
	}
	if err := consume(out, &msg, model, resp.Body); err != nil {
		fail(err)
		return
	}
	if err := ctx.Err(); err != nil {
		fail(err)
		return
	}
	out.Push(ai.AssistantMessageEvent{Type: "done", Reason: msg.StopReason, Message: snapshot(msg)})
}

func snapshot(msg ai.AssistantMessage) *ai.AssistantMessage {
	msg.Content = append([]ai.AssistantContentBlock(nil), msg.Content...)
	// Arguments are assigned only when a tool block ends; never mutated in place.
	return &msg
}

type blockState struct {
	index    int
	input    strings.Builder
	redacted []byte
	ended    bool
}

type wireEvent struct {
	ContentBlockIndex int `json:"contentBlockIndex"`
	Start             struct {
		ToolUse *struct {
			ID   string `json:"toolUseId"`
			Name string `json:"name"`
		} `json:"toolUse"`
	} `json:"start"`
	Delta struct {
		Text    *string `json:"text"`
		ToolUse *struct {
			Input string `json:"input"`
		} `json:"toolUse"`
		Reasoning *struct {
			Text      string `json:"text"`
			Signature string `json:"signature"`
			Redacted  []byte `json:"redactedContent"`
		} `json:"reasoningContent"`
	} `json:"delta"`
	StopReason string `json:"stopReason"`
	Usage      struct {
		Input      int `json:"inputTokens"`
		Output     int `json:"outputTokens"`
		Total      int `json:"totalTokens"`
		CacheRead  int `json:"cacheReadInputTokens"`
		CacheWrite int `json:"cacheWriteInputTokens"`
	} `json:"usage"`
}

func header(event eventstream.Message, key string) string {
	if v := event.Headers.Get(key); v != nil {
		return v.String()
	}
	return ""
}

func consume(out *ai.AssistantMessageEventStream, msg *ai.AssistantMessage, model ai.Model, reader io.Reader) error {
	decoder := eventstream.NewDecoder()
	blocks := map[int]*blockState{}
	started, stopped := false, false
	emit := func(kind string, index int, delta, content string) {
		e := ai.AssistantMessageEvent{Type: kind, ContentIndex: index, Delta: delta, Content: content, Partial: snapshot(*msg)}
		if kind == "toolcall_end" {
			tc, _ := msg.Content[index].AsToolCall()
			e.ToolCall = &tc
		}
		out.Push(e)
	}
	newBlock := func(wireIndex int, block ai.AssistantContentBlock) *blockState {
		state := &blockState{index: len(msg.Content)}
		blocks[wireIndex] = state
		msg.Content = append(msg.Content, block)
		kind := block.Type
		if kind == "toolCall" {
			kind = "toolcall"
		}
		emit(kind+"_start", state.index, "", "")
		return state
	}
	for {
		event, err := decoder.Decode(reader, nil)
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("Bedrock stream: %w", err)
		}
		kind := header(event, ":event-type")
		if header(event, ":message-type") == "exception" || header(event, ":message-type") == "error" {
			return fmt.Errorf("Bedrock %s: %s", header(event, ":exception-type"), event.Payload)
		}
		var e wireEvent
		if err := json.Unmarshal(event.Payload, &e); err != nil {
			return fmt.Errorf("invalid Bedrock %s event: %w", kind, err)
		}
		switch kind {
		case "messageStart":
			if started {
				return fmt.Errorf("duplicate Bedrock messageStart")
			}
			started = true
			emit("start", 0, "", "")
		case "contentBlockStart":
			if !started || stopped || blocks[e.ContentBlockIndex] != nil {
				return fmt.Errorf("unexpected Bedrock contentBlockStart")
			}
			if t := e.Start.ToolUse; t != nil {
				newBlock(e.ContentBlockIndex, ai.ToolCallBlock(ai.ToolCall{ID: t.ID, Name: t.Name, Arguments: object{}}))
			}
		case "contentBlockDelta":
			if !started || stopped {
				return fmt.Errorf("unexpected Bedrock contentBlockDelta")
			}
			state := blocks[e.ContentBlockIndex]
			if state != nil && state.ended {
				return fmt.Errorf("Bedrock delta after contentBlockStop")
			}
			switch {
			case e.Delta.Text != nil:
				if state == nil {
					state = newBlock(e.ContentBlockIndex, ai.TextBlock(""))
				}
				if msg.Content[state.index].Type != "text" {
					return fmt.Errorf("Bedrock block changed type")
				}
				msg.Content[state.index].Text += *e.Delta.Text
				emit("text_delta", state.index, *e.Delta.Text, "")
			case e.Delta.ToolUse != nil:
				if state == nil || msg.Content[state.index].Type != "toolCall" {
					return fmt.Errorf("Bedrock tool delta without tool start")
				}
				state.input.WriteString(e.Delta.ToolUse.Input)
				emit("toolcall_delta", state.index, e.Delta.ToolUse.Input, "")
			case e.Delta.Reasoning != nil:
				if state == nil {
					state = newBlock(e.ContentBlockIndex, ai.ThinkingBlock(""))
				}
				b := &msg.Content[state.index]
				if b.Type != "thinking" {
					return fmt.Errorf("Bedrock block changed type")
				}
				r := e.Delta.Reasoning
				b.Thinking += r.Text
				b.ThinkingSignature += r.Signature
				if len(r.Redacted) > 0 {
					state.redacted = append(state.redacted, r.Redacted...)
					b.Redacted = true
					b.ThinkingSignature = base64.StdEncoding.EncodeToString(state.redacted)
				}
				if r.Text != "" {
					emit("thinking_delta", state.index, r.Text, "")
				}
			}
		case "contentBlockStop":
			state := blocks[e.ContentBlockIndex]
			if state == nil || state.ended {
				return fmt.Errorf("unexpected Bedrock contentBlockStop")
			}
			state.ended = true
			b := &msg.Content[state.index]
			switch b.Type {
			case "text":
				emit("text_end", state.index, "", b.Text)
			case "thinking":
				emit("thinking_end", state.index, "", b.Thinking)
			case "toolCall":
				args := object{}
				if state.input.Len() > 0 {
					if err := json.Unmarshal([]byte(state.input.String()), &args); err != nil || args == nil {
						return fmt.Errorf("invalid Bedrock tool arguments for %q", b.Name)
					}
				}
				b.Arguments = args
				emit("toolcall_end", state.index, "", "")
			}
		case "messageStop":
			if !started || stopped {
				return fmt.Errorf("unexpected Bedrock messageStop")
			}
			stopped = true
			msg.RawStopReason = e.StopReason
			switch e.StopReason {
			case "end_turn", "stop_sequence":
				msg.StopReason = ai.StopStop
			case "max_tokens", "model_context_window_exceeded":
				msg.StopReason = ai.StopLength
			case "tool_use":
				msg.StopReason = ai.StopToolUse
			default:
				return fmt.Errorf("Bedrock stopped: %s", e.StopReason)
			}
		case "metadata":
			msg.Usage = ai.Usage{Input: e.Usage.Input, Output: e.Usage.Output, TotalTokens: e.Usage.Total, CacheRead: e.Usage.CacheRead, CacheWrite: e.Usage.CacheWrite}
			msg.Usage.Cost = ai.CalculateCost(model.Cost, msg.Usage)
		}
	}
	if !stopped {
		return fmt.Errorf("Bedrock stream ended without messageStop")
	}
	for _, state := range blocks {
		if !state.ended {
			return fmt.Errorf("Bedrock stream ended without contentBlockStop")
		}
	}
	return nil
}

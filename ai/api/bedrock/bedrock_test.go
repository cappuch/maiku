package bedrock

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream"
	"github.com/cappuch/maiku/ai"
)

type testEvent struct{ kind, payload string }

func encodeEvents(t *testing.T, events ...testEvent) []byte {
	t.Helper()
	var buf bytes.Buffer
	encoder := eventstream.NewEncoder()
	for _, e := range events {
		var headers eventstream.Headers
		headers.Set(":message-type", eventstream.StringValue("event"))
		headers.Set(":event-type", eventstream.StringValue(e.kind))
		if err := encoder.Encode(&buf, eventstream.Message{Headers: headers, Payload: []byte(e.payload)}); err != nil {
			t.Fatal(err)
		}
	}
	return buf.Bytes()
}

func testModel() ai.Model {
	return ai.Model{ID: "us.anthropic.claude-sonnet-4-20250514-v1:0", API: ai.APIBedrockConverseStream, Provider: ProviderID, Reasoning: true}
}

func TestStreamTextThinkingToolsAndUsage(t *testing.T) {
	events := encodeEvents(t,
		testEvent{"messageStart", `{"role":"assistant"}`},
		testEvent{"contentBlockDelta", `{"contentBlockIndex":0,"delta":{"reasoningContent":{"text":"think"}}}`},
		testEvent{"contentBlockDelta", `{"contentBlockIndex":0,"delta":{"reasoningContent":{"signature":"signed"}}}`},
		testEvent{"contentBlockStop", `{"contentBlockIndex":0}`},
		testEvent{"contentBlockDelta", `{"contentBlockIndex":1,"delta":{"text":"Hello"}}`},
		testEvent{"contentBlockDelta", `{"contentBlockIndex":1,"delta":{"text":" world"}}`},
		testEvent{"contentBlockStop", `{"contentBlockIndex":1}`},
		testEvent{"contentBlockStart", `{"contentBlockIndex":2,"start":{"toolUse":{"toolUseId":"call_1","name":"read"}}}`},
		testEvent{"contentBlockDelta", `{"contentBlockIndex":2,"delta":{"toolUse":{"input":"{\"path\":"}}}`},
		testEvent{"contentBlockDelta", `{"contentBlockIndex":2,"delta":{"toolUse":{"input":"\"file.go\"}"}}}`},
		testEvent{"contentBlockStop", `{"contentBlockIndex":2}`},
		testEvent{"messageStop", `{"stopReason":"tool_use"}`},
		testEvent{"metadata", `{"usage":{"inputTokens":10,"outputTokens":20,"totalTokens":42,"cacheReadInputTokens":7,"cacheWriteInputTokens":5}}`},
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing bearer authentication")
		}
		if !strings.HasSuffix(r.URL.Path, "/converse-stream") {
			t.Errorf("path = %s", r.URL.Path)
		}
		var payload object
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload["testHook"] != true {
			t.Error("payload hook was not applied")
		}
		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		_, _ = w.Write(events)
	}))
	defer server.Close()
	model := testModel()
	model.BaseURL = server.URL
	model.Cost = ai.ModelCost{ModelCostRates: ai.ModelCostRates{Input: 1, Output: 2, CacheRead: 0.1, CacheWrite: 1.25}}
	responseCalled := false
	opts := &ai.SimpleStreamOptions{StreamOptions: ai.StreamOptions{APIKey: "test-token", OnPayload: func(payload any, _ ai.Model) (any, error) { payload.(object)["testHook"] = true; return payload, nil }, OnResponse: func(resp ai.ProviderResponse, _ ai.Model) { responseCalled = resp.Status == 200 }}}
	stream := Stream(model, ai.Context{Messages: []ai.Message{ai.FromUser(ai.UserMessage{Content: "hi"})}}, opts)
	var firstText *ai.AssistantMessage
	var toolEnd *ai.ToolCall
	for e := range stream.Iter() {
		if e.Type == "text_delta" && firstText == nil {
			firstText = e.Partial
		}
		if e.Type == "toolcall_end" {
			toolEnd = e.ToolCall
		}
	}
	msg := stream.Result()
	if msg.StopReason != ai.StopToolUse {
		t.Fatalf("result: %+v", msg)
	}
	if !responseCalled || ai.AssistantText(msg) != "Hello world" {
		t.Fatalf("response: %+v", msg)
	}
	if msg.Content[0].ThinkingSignature != "signed" || msg.Content[0].Thinking != "think" {
		t.Fatal(msg.Content[0])
	}
	if firstText.Content[1].Text != "Hello" {
		t.Fatal("previous partial message was mutated")
	}
	if toolEnd == nil || toolEnd.Arguments["path"] != "file.go" {
		t.Fatalf("tool = %+v", toolEnd)
	}
	if msg.Usage.Input != 10 || msg.Usage.CacheRead != 7 || msg.Usage.CacheWrite != 5 || msg.Usage.TotalTokens != 42 || msg.Usage.Cost.Total == 0 {
		t.Fatal(msg.Usage)
	}
	// Signed reasoning and completed tool input survive the next request.
	payload, err := buildRequest(model, ai.Context{Messages: []ai.Message{ai.FromAssistant(msg)}}, &ai.SimpleStreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(payload)
	if !bytes.Contains(encoded, []byte(`"signature":"signed"`)) || !bytes.Contains(encoded, []byte(`"path":"file.go"`)) {
		t.Fatal(string(encoded))
	}
}

func TestStreamRejectsIncompleteAndMalformedEvents(t *testing.T) {
	cases := map[string][]testEvent{
		"empty":              {},
		"truncated":          {{"messageStart", `{}`}, {"contentBlockDelta", `{"delta":{"text":"partial"}}`}},
		"invalid json":       {{"messageStart", `{`}},
		"invalid tool input": {{"messageStart", `{}`}, {"contentBlockStart", `{"start":{"toolUse":{"toolUseId":"1","name":"read"}}}`}, {"contentBlockDelta", `{"delta":{"toolUse":{"input":"{"}}}`}, {"contentBlockStop", `{}`}},
		"guardrail":          {{"messageStart", `{}`}, {"messageStop", `{"stopReason":"guardrail_intervened"}`}},
	}
	for name, events := range cases {
		t.Run(name, func(t *testing.T) {
			msg := ai.AssistantMessage{}
			err := consume(ai.NewAssistantMessageEventStream(), &msg, testModel(), bytes.NewReader(encodeEvents(t, events...)))
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
	data := encodeEvents(t, testEvent{"messageStart", `{}`})
	data[len(data)-1] ^= 0xff
	if err := consume(ai.NewAssistantMessageEventStream(), &ai.AssistantMessage{}, testModel(), bytes.NewReader(data)); err == nil {
		t.Fatal("accepted corrupt CRC")
	}
}

func TestStreamException(t *testing.T) {
	var buf bytes.Buffer
	var headers eventstream.Headers
	headers.Set(":message-type", eventstream.StringValue("exception"))
	headers.Set(":exception-type", eventstream.StringValue("throttlingException"))
	if err := eventstream.NewEncoder().Encode(&buf, eventstream.Message{Headers: headers, Payload: []byte(`{"message":"slow down"}`)}); err != nil {
		t.Fatal(err)
	}
	err := consume(ai.NewAssistantMessageEventStream(), &ai.AssistantMessage{}, testModel(), &buf)
	if err == nil || !strings.Contains(err.Error(), "throttlingException") {
		t.Fatal(err)
	}
}

func TestRequestImagesAndToolResults(t *testing.T) {
	model := testModel()
	data := ai.Context{SystemPrompt: "system", Tools: []ai.Tool{{Name: "read"}}, Messages: []ai.Message{
		ai.FromUser(ai.UserMessage{Content: []any{ai.TextContent{Type: "text", Text: "look"}, ai.ImageContent{Type: "image", MimeType: "image/png", Data: "aGk="}}}),
		ai.FromAssistant(ai.AssistantMessage{Content: []ai.AssistantContentBlock{ai.ToolCallBlock(ai.ToolCall{ID: "call|1", Name: "read"}), ai.ToolCallBlock(ai.ToolCall{ID: "call|2", Name: "read"})}}),
		ai.FromToolResult(ai.ToolResultMessage{ToolCallID: "call|1", IsError: true, Content: []ai.ToolResultContent{{Type: "text", Text: "failed"}}}),
		ai.FromToolResult(ai.ToolResultMessage{ToolCallID: "call|2", Content: []ai.ToolResultContent{{Type: "image", MimeType: "image/jpeg", Data: "aGk="}}}),
	}}
	req, err := buildRequest(model, data, &ai.SimpleStreamOptions{Reasoning: ai.ThinkingLow})
	if err != nil {
		t.Fatal(err)
	}
	messages := req["messages"].([]message)
	if len(messages) != 3 || len(messages[2].Content) != 2 {
		t.Fatal(messages)
	}
	encoded, _ := json.Marshal(req)
	for _, part := range []string{`"toolUseId":"call_1"`, `"status":"error"`, `"format":"png"`, `"bytes":"aGk="`, `"budget_tokens":2048`, `"inputSchema":{"json":{"type":"object","properties":{}}}`} {
		if !strings.Contains(string(encoded), part) {
			t.Errorf("missing %s in %s", part, encoded)
		}
	}
	data.Messages[0].UserContent = []ai.ImageContent{{Type: "image", MimeType: "image/png", Data: "invalid!"}}
	if _, err := buildRequest(model, data, &ai.SimpleStreamOptions{}); err == nil {
		t.Fatal("accepted invalid base64")
	}
}

func TestSignedRequestAndCancellation(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "test-access")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test-secret")
	t.Setenv("AWS_SESSION_TOKEN", "test-session")
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "")
	t.Setenv("AWS_REGION", "eu-west-1")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Authorization"), "/eu-west-1/bedrock/aws4_request") || r.Header.Get("X-Amz-Security-Token") != "test-session" {
			t.Error("missing AWS signature/session token")
		}
		w.WriteHeader(400)
		_, _ = fmt.Fprint(w, `{"message":"bad model"}`)
	}))
	defer server.Close()
	model := testModel()
	model.BaseURL = server.URL
	msg := Stream(model, ai.Context{}, nil).Result()
	if msg.StopReason != ai.StopError || !strings.Contains(msg.ErrorMessage, "bad model") {
		t.Fatal(msg)
	}
	signal := make(chan struct{})
	close(signal)
	msg = Stream(model, ai.Context{}, &ai.SimpleStreamOptions{StreamOptions: ai.StreamOptions{Signal: signal}}).Result()
	if msg.StopReason != ai.StopAborted {
		t.Fatal(msg)
	}
}

func TestCancellationDuringStream(t *testing.T) {
	started := make(chan struct{})
	signal := make(chan struct{})
	data := encodeEvents(t, testEvent{"messageStart", `{}`})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(data)
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	model := testModel()
	model.BaseURL = server.URL
	stream := Stream(model, ai.Context{}, &ai.SimpleStreamOptions{StreamOptions: ai.StreamOptions{APIKey: "test", Signal: signal}})
	<-started
	close(signal)
	done := make(chan ai.AssistantMessage, 1)
	go func() { done <- stream.Result() }()
	select {
	case msg := <-done:
		if msg.StopReason != ai.StopAborted {
			t.Fatal(msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation did not stop stream")
	}
}

func TestFetchModelsAndProfiles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test" {
			t.Error("missing model catalog authentication")
		}
		switch r.URL.Path {
		case "/foundation-models":
			_, _ = fmt.Fprint(w, `{"modelSummaries":[{"modelId":"anthropic.claude-sonnet-4-test","modelName":"Claude","inputModalities":["TEXT","IMAGE"],"outputModalities":["TEXT"],"responseStreamingSupported":true,"inferenceTypesSupported":["ON_DEMAND"]},{"modelId":"embed","outputModalities":["EMBEDDING"]}]}`)
		case "/inference-profiles":
			if r.URL.Query().Get("nextToken") == "next page" {
				_, _ = fmt.Fprint(w, `{"inferenceProfileSummaries":[{"inferenceProfileId":"inactive","status":"INACTIVE"}]}`)
			} else {
				_, _ = fmt.Fprint(w, `{"nextToken":"next page","inferenceProfileSummaries":[{"inferenceProfileId":"us.anthropic.claude-sonnet-4-test","inferenceProfileName":"US Claude","status":"ACTIVE","models":[{"modelArn":"arn:aws:bedrock:us-east-1::foundation-model/anthropic.claude-sonnet-4-test"}]}]}`)
			}
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	models, err := FetchModels(context.Background(), server.URL, "test")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[1].ID != "us.anthropic.claude-sonnet-4-test" || !models[1].Reasoning || len(models[1].Input) != 2 || models[1].API != ai.APIBedrockConverseStream {
		t.Fatal(models)
	}
}

func TestAdaptiveThinking(t *testing.T) {
	for _, id := range []string{"anthropic.claude-opus-4-6-v1", "us.anthropic.claude-sonnet-4-6", "anthropic.claude-opus-4-7", "anthropic.claude-sonnet-5"} {
		model := testModel()
		model.ID = id
		req, err := buildRequest(model, ai.Context{}, &ai.SimpleStreamOptions{Reasoning: ai.ThinkingMedium})
		if err != nil {
			t.Fatal(err)
		}
		fields := req["additionalModelRequestFields"].(object)
		if fields["thinking"].(object)["type"] != "adaptive" || fields["output_config"].(object)["effort"] != "medium" {
			t.Fatalf("%s: %v", id, fields)
		}
		req, err = buildRequest(model, ai.Context{}, &ai.SimpleStreamOptions{Reasoning: ai.ThinkingOff})
		if err != nil {
			t.Fatal(err)
		}
		if req["additionalModelRequestFields"].(object)["thinking"].(object)["type"] != "disabled" {
			t.Fatal("thinking off ignored")
		}
	}
	for _, id := range []string{"anthropic.claude-sonnet-4-20250514-v1:0", "anthropic.claude-opus-4-5-20251101-v1:0", "anthropic.claude-3-7-sonnet"} {
		if adaptiveThinking(id) {
			t.Errorf("older model classified as adaptive: %s", id)
		}
	}
}

func TestAdaptiveThinkingOutputCeiling(t *testing.T) {
	for _, tokens := range []int{4096, 128000} {
		for _, level := range []ai.ThinkingLevel{ai.ThinkingHigh, ai.ThinkingXHigh, ai.ThinkingMax} {
			for _, override := range []*int{nil, &tokens} {
				model := testModel()
				model.ID = "us.anthropic.claude-opus-5-5-v1:0"
				model.MaxTokens = tokens
				opts := &ai.SimpleStreamOptions{Reasoning: level}
				opts.MaxTokens = override
				req, err := buildRequest(model, ai.Context{}, opts)
				if err != nil {
					t.Fatal(err)
				}
				if got := req["inferenceConfig"].(object)["maxTokens"]; got != tokens {
					t.Fatalf("%s: maxTokens = %v, want %d", level, got, tokens)
				}
			}
		}
	}
}

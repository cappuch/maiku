package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/mikus/maiku/ai"
)

func testModel() ai.Model {
	return ai.Model{ID: "claude-sonnet-4-5", Provider: "anthropic", MaxTokens: 4096}
}

func mustBuild(t *testing.T, model ai.Model, ctx ai.Context, opts *ai.SimpleStreamOptions) *anthropicRequest {
	t.Helper()
	req, err := buildRequest(model, ctx, opts)
	if err != nil {
		t.Fatalf("buildRequest: %v", err)
	}
	return req
}

func countCacheControl(req *anthropicRequest) int {
	n := 0
	for _, s := range req.System {
		if len(s.CacheControl) > 0 {
			n++
		}
	}
	for _, t := range req.Tools {
		if len(t.CacheControl) > 0 {
			n++
		}
	}
	for _, m := range req.Messages {
		switch blocks := m.Content.(type) {
		case []any:
			for _, b := range blocks {
				switch blk := b.(type) {
				case anthropicTextBlock:
					if len(blk.CacheControl) > 0 {
						n++
					}
				case anthropicToolResultBlock:
					if len(blk.CacheControl) > 0 {
						n++
					}
				}
			}
		case []anthropicTextBlock:
			for _, blk := range blocks {
				if len(blk.CacheControl) > 0 {
					n++
				}
			}
		}
	}
	return n
}

func TestCacheControlDefaultShort(t *testing.T) {
	ctx := ai.Context{
		SystemPrompt: "you are maiku",
		Messages:     []ai.Message{{Role: "user", UserContent: "hello"}},
		Tools: []ai.Tool{
			{Name: "read", Description: "read files", Parameters: json.RawMessage(`{"type":"object"}`)},
			{Name: "bash", Description: "run commands", Parameters: json.RawMessage(`{"type":"object"}`)},
		},
	}
	req := mustBuild(t, testModel(), ctx, &ai.SimpleStreamOptions{})

	// Default retention resolves to "long" on first-party Anthropic.
	if len(req.System) != 1 || len(req.System[0].CacheControl) == 0 {
		t.Fatalf("expected cache_control on system, got %+v", req.System)
	}
	if string(req.System[0].CacheControl) != `{"type":"ephemeral","ttl":"1h"}` {
		t.Fatalf("unexpected system cache_control: %s", req.System[0].CacheControl)
	}
	// Only the LAST tool definition carries a breakpoint.
	for i, tool := range req.Tools {
		has := len(tool.CacheControl) > 0
		if want := i == len(req.Tools)-1; has != want {
			t.Fatalf("tool %d cache_control=%v want %v", i, has, want)
		}
	}
	if got := countCacheControl(req); got != 3 {
		t.Fatalf("expected 3 breakpoints (system+last tool+last message), got %d", got)
	}
}

func TestCacheControlLongTTL(t *testing.T) {
	ctx := ai.Context{
		SystemPrompt: "you are maiku",
		Messages:     []ai.Message{{Role: "user", UserContent: "hello"}},
	}

	opts := &ai.SimpleStreamOptions{}
	opts.CacheRetention = ai.CacheLong
	req := mustBuild(t, testModel(), ctx, opts)
	if string(req.System[0].CacheControl) != `{"type":"ephemeral","ttl":"1h"}` {
		t.Fatalf("unexpected long cache_control: %s", req.System[0].CacheControl)
	}

	// Explicit short stays short even on a capable endpoint.
	short := &ai.SimpleStreamOptions{}
	short.CacheRetention = ai.CacheShort
	reqShort := mustBuild(t, testModel(), ctx, short)
	if string(reqShort.System[0].CacheControl) != `{"type":"ephemeral"}` {
		t.Fatalf("expected plain ephemeral for explicit CacheShort, got %s", reqShort.System[0].CacheControl)
	}
}

// Third-party anthropic-compatible endpoints get the plain ephemeral marker
// unless they opt in via Compat["supportsLongCacheRetention"].
func TestCacheControlThirdPartyDefaultsShort(t *testing.T) {
	ctx := ai.Context{
		SystemPrompt: "you are maiku",
		Messages:     []ai.Message{{Role: "user", UserContent: "hello"}},
	}
	model := testModel()
	model.Provider = "minimax"
	model.BaseURL = "https://api.minimax.io/anthropic"

	req := mustBuild(t, model, ctx, &ai.SimpleStreamOptions{})
	if string(req.System[0].CacheControl) != `{"type":"ephemeral"}` {
		t.Fatalf("third-party default should be plain ephemeral, got %s", req.System[0].CacheControl)
	}

	optedIn := model
	optedIn.Compat = map[string]any{"supportsLongCacheRetention": true}
	reqLong := mustBuild(t, optedIn, ctx, &ai.SimpleStreamOptions{})
	if string(reqLong.System[0].CacheControl) != `{"type":"ephemeral","ttl":"1h"}` {
		t.Fatalf("Compat opt-in should grant 1h ttl, got %s", reqLong.System[0].CacheControl)
	}
}

func TestCacheControlNone(t *testing.T) {
	ctx := ai.Context{
		SystemPrompt: "you are maiku",
		Messages:     []ai.Message{{Role: "user", UserContent: "hello"}, {Role: "assistant", AssistantContent: []ai.AssistantContentBlock{ai.TextBlock("hi")}}},
		Tools:        []ai.Tool{{Name: "read", Parameters: json.RawMessage(`{"type":"object"}`)}},
	}
	req := mustBuild(t, testModel(), ctx, &ai.SimpleStreamOptions{})
	// The rolling breakpoint lands on the newest USER message even though an
	// assistant turn trails it: prefix matching makes moving the marker
	// forward pure write cost.
	if got := countCacheControl(req); got != 3 {
		t.Fatalf("expected 3 breakpoints (system+last tool+last user msg), got %d", got)
	}
	firstMsg := req.Messages[0]
	blocks, ok := firstMsg.Content.([]any)
	if !ok || len(blocks) == 0 {
		t.Fatalf("expected first message []any content, got %T", firstMsg.Content)
	}
	if textBlock, ok := blocks[0].(anthropicTextBlock); !ok || len(textBlock.CacheControl) == 0 {
		t.Fatalf("expected marker on first user block, got %T", blocks[0])
	}

	none := &ai.SimpleStreamOptions{}
	none.CacheRetention = ai.CacheNone
	reqNone := mustBuild(t, testModel(), ctx, none)
	if len(reqNone.System) == 1 && len(reqNone.System[0].CacheControl) > 0 {
		t.Fatal("expected no cache_control with CacheNone")
	}
	if got := countCacheControl(reqNone); got != 0 {
		t.Fatalf("expected 0 breakpoints with CacheNone, got %d", got)
	}
}

func TestRollingLastMessageBreakpoint(t *testing.T) {
	ctx := ai.Context{
		Messages: []ai.Message{
			{Role: "user", UserContent: "first"},
			{Role: "assistant", AssistantContent: []ai.AssistantContentBlock{ai.TextBlock("a")}},
			{Role: "user", UserContent: "second"},
		},
	}
	req := mustBuild(t, testModel(), ctx, &ai.SimpleStreamOptions{})
	last := req.Messages[len(req.Messages)-1]
	blocks, ok := last.Content.([]any)
	if !ok || len(blocks) == 0 {
		t.Fatalf("expected last message []any content, got %T", last.Content)
	}
	lastBlock, ok := blocks[len(blocks)-1].(anthropicTextBlock)
	if !ok {
		t.Fatalf("expected last block text, got %T", blocks[len(blocks)-1])
	}
	if len(lastBlock.CacheControl) == 0 {
		t.Fatal("expected cache_control on last message block")
	}
	// No stop-assistant breakpoints on earlier blocks: verify only the last message has it.
	var guard int
	for _, m := range req.Messages {
		if blocks, ok := m.Content.([]any); ok {
			for _, block := range blocks {
				if textBlock, ok := block.(anthropicTextBlock); ok && len(textBlock.CacheControl) > 0 {
					guard++
				}
				if toolBlock, ok := block.(anthropicToolResultBlock); ok && len(toolBlock.CacheControl) > 0 {
					guard++
				}
			}
		}
	}
	if guard != 1 {
		t.Fatalf("expected exactly one message-block breakpoint, got %d", guard)
	}
}

func TestToolResultBreakpoint(t *testing.T) {
	ctx := ai.Context{
		Messages: []ai.Message{
			{Role: "user", UserContent: "run"},
			{Role: "assistant", AssistantContent: []ai.AssistantContentBlock{
				{Type: "toolCall", ID: "t1", Name: "bash", Arguments: map[string]any{"command": "ls"}},
			}},
			{Role: "toolResult", ToolCallID: "t1", ToolContent: []ai.ToolResultContent{{Type: "text", Text: "ok"}}},
		},
	}
	req := mustBuild(t, testModel(), ctx, &ai.SimpleStreamOptions{})
	last := req.Messages[len(req.Messages)-1]
	blocks, ok := last.Content.([]any)
	if !ok {
		t.Fatalf("tool-result turn should be []any, got %T", last.Content)
	}
	lastBlock, ok := blocks[len(blocks)-1].(anthropicToolResultBlock)
	if !ok {
		t.Fatalf("expected tool_result block, got %T (%+v)", blocks[len(blocks)-1], blocks)
	}
	if len(lastBlock.CacheControl) == 0 {
		t.Fatal("expected cache_control on last tool_result block")
	}
}

// A trailing assistant turn (e.g. an aborted empty shell that survived
// filtering) must not strand the rolling breakpoint: it stays on the last
// user message so the prefix below keeps cache-hitting.
func TestRollingBreakpointSurvivesTrailingAssistantTurn(t *testing.T) {
	ctx := ai.Context{
		Messages: []ai.Message{
			{Role: "user", UserContent: "first"},
			{Role: "assistant", AssistantContent: []ai.AssistantContentBlock{ai.TextBlock("a")}},
			{Role: "user", UserContent: "second"},
			{Role: "assistant", AssistantContent: []ai.AssistantContentBlock{ai.TextBlock("b")}},
		},
	}
	req := mustBuild(t, testModel(), ctx, &ai.SimpleStreamOptions{})
	last := req.Messages[len(req.Messages)-1]
	if blocks, ok := last.Content.([]any); ok {
		if textBlock, isText := blocks[0].(anthropicTextBlock); isText && len(textBlock.CacheControl) > 0 {
			t.Fatal("marker must not sit on the trailing assistant turn")
		}
	}
	userMsg := req.Messages[len(req.Messages)-2]
	blocks, ok := userMsg.Content.([]any)
	if !ok || len(blocks) == 0 {
		t.Fatalf("expected user message []any content, got %T", userMsg.Content)
	}
	textBlock, ok := blocks[len(blocks)-1].(anthropicTextBlock)
	if !ok || len(textBlock.CacheControl) == 0 {
		t.Fatalf("expected marker on last user message, got %T (%+v)", blocks[len(blocks)-1], blocks)
	}
}

func TestLongCacheRetentionCompatGate(t *testing.T) {
	ctx := ai.Context{
		SystemPrompt: "you are maiku",
		Messages:     []ai.Message{{Role: "user", UserContent: "hi"}},
	}
	model := testModel()
	model.Compat = map[string]any{"supportsLongCacheRetention": false}

	opts := &ai.SimpleStreamOptions{}
	opts.CacheRetention = ai.CacheLong
	req := mustBuild(t, model, ctx, opts)
	if len(req.System) != 1 || len(req.System[0].CacheControl) == 0 {
		t.Fatalf("expected system cache_control, got %+v", req.System)
	}
	if string(req.System[0].CacheControl) != `{"type":"ephemeral"}` {
		t.Fatalf("expected Compat opt-out to force plain ephemeral, got %s", req.System[0].CacheControl)
	}
}

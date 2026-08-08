// Package ai is a Go port of @earendil-works/pi-ai (core types and streaming contracts).
package ai

import "encoding/json"

// Known API identifiers.
const (
	APIOpenAICompletions     = "openai-completions"
	APIOpenAIResponses       = "openai-responses"
	APIAzureOpenAIResponses  = "azure-openai-responses"
	APIOpenAICodexResponses  = "openai-codex-responses"
	APIAnthropicMessages     = "anthropic-messages"
	APIBedrockConverseStream = "bedrock-converse-stream"
	APIGoogleGenerativeAI    = "google-generative-ai"
	APIGoogleVertex          = "google-vertex"
	APIMistralConversations  = "mistral-conversations"
	APIPiMessages            = "pi-messages"
)

// Thinking levels.
const (
	ThinkingOff     ThinkingLevel = "off"
	ThinkingMinimal ThinkingLevel = "minimal"
	ThinkingLow     ThinkingLevel = "low"
	ThinkingMedium  ThinkingLevel = "medium"
	ThinkingHigh    ThinkingLevel = "high"
	ThinkingXHigh   ThinkingLevel = "xhigh"
	ThinkingMax     ThinkingLevel = "max"
)

type ThinkingLevel string

type ThinkingLevelMap map[ThinkingLevel]*string

type ThinkingBudgets struct {
	Minimal *int `json:"minimal,omitempty"`
	Low     *int `json:"low,omitempty"`
	Medium  *int `json:"medium,omitempty"`
	High    *int `json:"high,omitempty"`
}

type CacheRetention string

const (
	CacheNone  CacheRetention = "none"
	CacheShort CacheRetention = "short"
	CacheLong  CacheRetention = "long"
)

type Transport string

const (
	TransportSSE             Transport = "sse"
	TransportWebSocket       Transport = "websocket"
	TransportWebSocketCached Transport = "websocket-cached"
	TransportAuto            Transport = "auto"
)

type StopReason string

const (
	StopPending  StopReason = "pending"
	StopStop     StopReason = "stop"
	StopLength   StopReason = "length"
	StopToolUse  StopReason = "toolUse"
	StopError    StopReason = "error"
	StopAborted  StopReason = "aborted"
	StopDeferred StopReason = "deferred"
)

// Content blocks

type TextContent struct {
	Type          string `json:"type"` // "text"
	Text          string `json:"text"`
	TextSignature string `json:"textSignature,omitempty"`
}

type ThinkingContent struct {
	Type              string `json:"type"` // "thinking"
	Thinking          string `json:"thinking"`
	ThinkingSignature string `json:"thinkingSignature,omitempty"`
	Redacted          bool   `json:"redacted,omitempty"`
}

type ImageContent struct {
	Type     string `json:"type"` // "image"
	Data     string `json:"data"`
	MimeType string `json:"mimeType"`
}

type ToolCall struct {
	Type             string         `json:"type"` // "toolCall"
	ID               string         `json:"id"`
	Name             string         `json:"name"`
	Arguments        map[string]any `json:"arguments"`
	ThoughtSignature string         `json:"thoughtSignature,omitempty"`
	Namespace        string         `json:"namespace,omitempty"`
}

// Usage / cost

type CostBreakdown struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
	Total      float64 `json:"total"`
}

type Usage struct {
	Input        int           `json:"input"`
	Output       int           `json:"output"`
	CacheRead    int           `json:"cacheRead"`
	CacheWrite   int           `json:"cacheWrite"`
	CacheWrite1h *int          `json:"cacheWrite1h,omitempty"`
	Reasoning    *int          `json:"reasoning,omitempty"`
	TotalTokens  int           `json:"totalTokens"`
	Cost         CostBreakdown `json:"cost"`
}

func EmptyUsage() Usage {
	return Usage{Cost: CostBreakdown{}}
}

// Messages

type DeferredHandle struct {
	Provider    string          `json:"provider"`
	ModelID     string          `json:"modelId"`
	API         string          `json:"api"`
	ID          string          `json:"id"`
	ExpiresAt   *int64          `json:"expiresAt,omitempty"`
	PollAfterMs *int64          `json:"pollAfterMs,omitempty"`
	Data        json.RawMessage `json:"data,omitempty"`
}

type UserMessage struct {
	Role      string `json:"role"`    // "user"
	Content   any    `json:"content"` // string | [](TextContent|ImageContent)
	Timestamp int64  `json:"timestamp"`
}

type AssistantContentBlock struct {
	Type string `json:"type"`
	// Populated based on Type
	Text              string         `json:"text,omitempty"`
	TextSignature     string         `json:"textSignature,omitempty"`
	Thinking          string         `json:"thinking,omitempty"`
	ThinkingSignature string         `json:"thinkingSignature,omitempty"`
	Redacted          bool           `json:"redacted,omitempty"`
	ID                string         `json:"id,omitempty"`
	Name              string         `json:"name,omitempty"`
	Arguments         map[string]any `json:"arguments,omitempty"`
	ThoughtSignature  string         `json:"thoughtSignature,omitempty"`
	Namespace         string         `json:"namespace,omitempty"`
}

func TextBlock(text string) AssistantContentBlock {
	return AssistantContentBlock{Type: "text", Text: text}
}

func ThinkingBlock(thinking string) AssistantContentBlock {
	return AssistantContentBlock{Type: "thinking", Thinking: thinking}
}

func ToolCallBlock(tc ToolCall) AssistantContentBlock {
	return AssistantContentBlock{
		Type:             "toolCall",
		ID:               tc.ID,
		Name:             tc.Name,
		Arguments:        tc.Arguments,
		ThoughtSignature: tc.ThoughtSignature,
		Namespace:        tc.Namespace,
	}
}

func (b AssistantContentBlock) AsToolCall() (ToolCall, bool) {
	if b.Type != "toolCall" {
		return ToolCall{}, false
	}
	return ToolCall{
		Type:             "toolCall",
		ID:               b.ID,
		Name:             b.Name,
		Arguments:        b.Arguments,
		ThoughtSignature: b.ThoughtSignature,
		Namespace:        b.Namespace,
	}, true
}

type AssistantMessage struct {
	Role          string                  `json:"role"` // "assistant"
	Content       []AssistantContentBlock `json:"content"`
	API           string                  `json:"api"`
	Provider      string                  `json:"provider"`
	Model         string                  `json:"model"`
	ResponseModel string                  `json:"responseModel,omitempty"`
	ResponseID    string                  `json:"responseId,omitempty"`
	Usage         Usage                   `json:"usage"`
	StopReason    StopReason              `json:"stopReason"`
	Deferred      *DeferredHandle         `json:"deferred,omitempty"`
	ErrorMessage  string                  `json:"errorMessage,omitempty"`
	RawStopReason string                  `json:"rawStopReason,omitempty"`
	EndTurn       *bool                   `json:"endTurn,omitempty"`
	Timestamp     int64                   `json:"timestamp"`
}

type ToolResultContent struct {
	Type     string `json:"type"` // "text" | "image"
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

type ToolResultMessage struct {
	Role           string              `json:"role"` // "toolResult"
	ToolCallID     string              `json:"toolCallId"`
	ToolName       string              `json:"toolName"`
	Content        []ToolResultContent `json:"content"`
	Details        any                 `json:"details,omitempty"`
	Usage          *Usage              `json:"usage,omitempty"`
	AddedToolNames []string            `json:"addedToolNames,omitempty"`
	IsError        bool                `json:"isError"`
	Timestamp      int64               `json:"timestamp"`
}

// Message is a tagged union of user / assistant / toolResult.
type Message struct {
	Role string `json:"role"`

	// User
	UserContent any `json:"content,omitempty"`

	// Assistant fields
	AssistantContent []AssistantContentBlock `json:"-"`
	API              string                  `json:"api,omitempty"`
	Provider         string                  `json:"provider,omitempty"`
	Model            string                  `json:"model,omitempty"`
	ResponseModel    string                  `json:"responseModel,omitempty"`
	ResponseID       string                  `json:"responseId,omitempty"`
	Usage            *Usage                  `json:"usage,omitempty"`
	StopReason       StopReason              `json:"stopReason,omitempty"`
	Deferred         *DeferredHandle         `json:"deferred,omitempty"`
	ErrorMessage     string                  `json:"errorMessage,omitempty"`
	RawStopReason    string                  `json:"rawStopReason,omitempty"`
	EndTurn          *bool                   `json:"endTurn,omitempty"`

	// Tool result
	ToolCallID     string              `json:"toolCallId,omitempty"`
	ToolName       string              `json:"toolName,omitempty"`
	ToolContent    []ToolResultContent `json:"-"`
	Details        any                 `json:"details,omitempty"`
	AddedToolNames []string            `json:"addedToolNames,omitempty"`
	IsError        bool                `json:"isError,omitempty"`

	Timestamp int64 `json:"timestamp"`
}

func FromUser(m UserMessage) Message {
	return Message{Role: "user", UserContent: m.Content, Timestamp: m.Timestamp}
}

func FromAssistant(m AssistantMessage) Message {
	return Message{
		Role:             "assistant",
		AssistantContent: m.Content,
		API:              m.API,
		Provider:         m.Provider,
		Model:            m.Model,
		ResponseModel:    m.ResponseModel,
		ResponseID:       m.ResponseID,
		Usage:            &m.Usage,
		StopReason:       m.StopReason,
		Deferred:         m.Deferred,
		ErrorMessage:     m.ErrorMessage,
		RawStopReason:    m.RawStopReason,
		EndTurn:          m.EndTurn,
		Timestamp:        m.Timestamp,
	}
}

func FromToolResult(m ToolResultMessage) Message {
	return Message{
		Role:           "toolResult",
		ToolCallID:     m.ToolCallID,
		ToolName:       m.ToolName,
		ToolContent:    m.Content,
		Details:        m.Details,
		Usage:          m.Usage,
		AddedToolNames: m.AddedToolNames,
		IsError:        m.IsError,
		Timestamp:      m.Timestamp,
	}
}

func (m Message) AsAssistant() (AssistantMessage, bool) {
	if m.Role != "assistant" {
		return AssistantMessage{}, false
	}
	usage := EmptyUsage()
	if m.Usage != nil {
		usage = *m.Usage
	}
	return AssistantMessage{
		Role:          "assistant",
		Content:       m.AssistantContent,
		API:           m.API,
		Provider:      m.Provider,
		Model:         m.Model,
		ResponseModel: m.ResponseModel,
		ResponseID:    m.ResponseID,
		Usage:         usage,
		StopReason:    m.StopReason,
		Deferred:      m.Deferred,
		ErrorMessage:  m.ErrorMessage,
		RawStopReason: m.RawStopReason,
		EndTurn:       m.EndTurn,
		Timestamp:     m.Timestamp,
	}, true
}

func (m Message) AsUser() (UserMessage, bool) {
	if m.Role != "user" {
		return UserMessage{}, false
	}
	return UserMessage{Role: "user", Content: m.UserContent, Timestamp: m.Timestamp}, true
}

func (m Message) AsToolResult() (ToolResultMessage, bool) {
	if m.Role != "toolResult" {
		return ToolResultMessage{}, false
	}
	return ToolResultMessage{
		Role:           "toolResult",
		ToolCallID:     m.ToolCallID,
		ToolName:       m.ToolName,
		Content:        m.ToolContent,
		Details:        m.Details,
		Usage:          m.Usage,
		AddedToolNames: m.AddedToolNames,
		IsError:        m.IsError,
		Timestamp:      m.Timestamp,
	}, true
}

// ContentText extracts concatenated text from a user message content value.
func ContentText(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var out string
		for _, item := range v {
			switch item := item.(type) {
			case string:
				out += item
			case TextContent:
				out += item.Text
			case map[string]any:
				if item["type"] == "text" {
					if t, ok := item["text"].(string); ok {
						out += t
					}
				}
			}
		}
		return out
	case []TextContent:
		var out string
		for _, t := range v {
			out += t.Text
		}
		return out
	default:
		return ""
	}
}

// AssistantText concatenates text blocks from an assistant message.
func AssistantText(m AssistantMessage) string {
	var out string
	for _, b := range m.Content {
		if b.Type == "text" {
			out += b.Text
		}
	}
	return out
}

// Tools / context

type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"` // JSON Schema
}

type Context struct {
	SystemPrompt string    `json:"systemPrompt,omitempty"`
	Messages     []Message `json:"messages"`
	Tools        []Tool    `json:"tools,omitempty"`
}

// Model

type ModelCostRates struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
}

type ModelCostTier struct {
	ModelCostRates
	InputTokensAbove int `json:"inputTokensAbove"`
}

type ModelCost struct {
	ModelCostRates
	Tiers []ModelCostTier `json:"tiers,omitempty"`
}

type Model struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	API              string            `json:"api"`
	Provider         string            `json:"provider"`
	BaseURL          string            `json:"baseUrl"`
	Reasoning        bool              `json:"reasoning"`
	ThinkingLevelMap ThinkingLevelMap  `json:"thinkingLevelMap,omitempty"`
	Input            []string          `json:"input"` // "text", "image"
	Cost             ModelCost         `json:"cost"`
	ContextWindow    int               `json:"contextWindow"`
	MaxTokens        int               `json:"maxTokens"`
	SamplingParams   map[string]any    `json:"samplingParams,omitempty"`
	Headers          map[string]string `json:"headers,omitempty"`
	Compat           map[string]any    `json:"compat,omitempty"`
}

// Stream options

type ProviderResponse struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
}

type StreamOptions struct {
	APIKey                    string
	Headers                   map[string]*string
	Env                       map[string]string
	Signal                    <-chan struct{} // closed = abort
	Temperature               *float64
	SamplingParams            map[string]any
	MaxTokens                 *int
	Transport                 Transport
	CacheRetention            CacheRetention
	SessionID                 string
	TimeoutMs                 *int
	MaxRetries                *int
	MaxRetryDelayMs           *int
	Metadata                  map[string]any
	OnPayload                 func(payload any, model Model) (any, error)
	OnResponse                func(resp ProviderResponse, model Model)
	WebsocketConnectTimeoutMs *int
}

type SimpleStreamOptions struct {
	StreamOptions
	Reasoning       ThinkingLevel
	Deferred        bool
	ThinkingBudgets *ThinkingBudgets
}

// AssistantMessageEvent is the streaming protocol for assistant responses.
type AssistantMessageEvent struct {
	Type         string            `json:"type"`
	Partial      *AssistantMessage `json:"partial,omitempty"`
	ContentIndex int               `json:"contentIndex,omitempty"`
	Delta        string            `json:"delta,omitempty"`
	Content      string            `json:"content,omitempty"`
	ToolCall     *ToolCall         `json:"toolCall,omitempty"`
	Reason       StopReason        `json:"reason,omitempty"`
	Message      *AssistantMessage `json:"message,omitempty"`
	Error        *AssistantMessage `json:"error,omitempty"`
}

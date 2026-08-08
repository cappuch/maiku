package core

import (
	"encoding/json"

	"github.com/mikus/maiku/ai"
)

// ai.Message keeps assistant and tool-result content in Go-only fields that
// are tagged `json:"-"`, so the struct cannot be marshalled directly without
// losing content. messageWire is the on-the-wire shape used for session
// files and the --mode json event stream: a single "content" field whose
// meaning depends on the role, matching the TypeScript message format.
type messageWire struct {
	Role string `json:"role"`

	Content json.RawMessage `json:"content,omitempty"`

	API           string             `json:"api,omitempty"`
	Provider      string             `json:"provider,omitempty"`
	Model         string             `json:"model,omitempty"`
	ResponseModel string             `json:"responseModel,omitempty"`
	ResponseID    string             `json:"responseId,omitempty"`
	Usage         *ai.Usage          `json:"usage,omitempty"`
	StopReason    ai.StopReason      `json:"stopReason,omitempty"`
	Deferred      *ai.DeferredHandle `json:"deferred,omitempty"`
	ErrorMessage  string             `json:"errorMessage,omitempty"`
	RawStopReason string             `json:"rawStopReason,omitempty"`
	EndTurn       *bool              `json:"endTurn,omitempty"`

	ToolCallID     string   `json:"toolCallId,omitempty"`
	ToolName       string   `json:"toolName,omitempty"`
	Details        any      `json:"details,omitempty"`
	AddedToolNames []string `json:"addedToolNames,omitempty"`
	IsError        bool     `json:"isError,omitempty"`

	Timestamp int64 `json:"timestamp"`
}

// EncodeMessage serializes an agent message to its JSON wire form.
func EncodeMessage(m ai.Message) ([]byte, error) {
	wire := messageWire{
		Role:           m.Role,
		API:            m.API,
		Provider:       m.Provider,
		Model:          m.Model,
		ResponseModel:  m.ResponseModel,
		ResponseID:     m.ResponseID,
		Usage:          m.Usage,
		StopReason:     m.StopReason,
		Deferred:       m.Deferred,
		ErrorMessage:   m.ErrorMessage,
		RawStopReason:  m.RawStopReason,
		EndTurn:        m.EndTurn,
		ToolCallID:     m.ToolCallID,
		ToolName:       m.ToolName,
		Details:        m.Details,
		AddedToolNames: m.AddedToolNames,
		IsError:        m.IsError,
		Timestamp:      m.Timestamp,
	}

	var content any
	switch m.Role {
	case "assistant":
		content = m.AssistantContent
	case "toolResult":
		content = m.ToolContent
	default:
		content = m.UserContent
	}
	if content != nil {
		raw, err := json.Marshal(content)
		if err != nil {
			return nil, err
		}
		wire.Content = raw
	}

	return json.Marshal(wire)
}

// DecodeMessage parses a message from its JSON wire form.
func DecodeMessage(data []byte) (ai.Message, error) {
	var wire messageWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return ai.Message{}, err
	}

	m := ai.Message{
		Role:           wire.Role,
		API:            wire.API,
		Provider:       wire.Provider,
		Model:          wire.Model,
		ResponseModel:  wire.ResponseModel,
		ResponseID:     wire.ResponseID,
		Usage:          wire.Usage,
		StopReason:     wire.StopReason,
		Deferred:       wire.Deferred,
		ErrorMessage:   wire.ErrorMessage,
		RawStopReason:  wire.RawStopReason,
		EndTurn:        wire.EndTurn,
		ToolCallID:     wire.ToolCallID,
		ToolName:       wire.ToolName,
		Details:        wire.Details,
		AddedToolNames: wire.AddedToolNames,
		IsError:        wire.IsError,
		Timestamp:      wire.Timestamp,
	}

	if len(wire.Content) == 0 {
		return m, nil
	}

	switch wire.Role {
	case "assistant":
		var blocks []ai.AssistantContentBlock
		if err := json.Unmarshal(wire.Content, &blocks); err != nil {
			return ai.Message{}, err
		}
		m.AssistantContent = blocks
	case "toolResult":
		var blocks []ai.ToolResultContent
		if err := json.Unmarshal(wire.Content, &blocks); err != nil {
			return ai.Message{}, err
		}
		m.ToolContent = blocks
	default:
		var content any
		if err := json.Unmarshal(wire.Content, &content); err != nil {
			return ai.Message{}, err
		}
		m.UserContent = content
	}

	return m, nil
}

// MessageJSON renders a message as a raw JSON value so it can be embedded
// inside larger event objects.
func MessageJSON(m ai.Message) json.RawMessage {
	raw, err := EncodeMessage(m)
	if err != nil {
		return json.RawMessage(`null`)
	}
	return json.RawMessage(raw)
}

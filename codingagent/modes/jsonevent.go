// Package modes implements the non-interactive CLI run modes.
package modes

import (
	"encoding/json"

	"github.com/mikus/maiku/agent"
	"github.com/mikus/maiku/ai"
	"github.com/mikus/maiku/codingagent/core"
)

// JSONEvent is the wire shape written to stdout by --mode json. Fields are
// populated according to Type, mirroring the TypeScript session event union.
type JSONEvent struct {
	Type string `json:"type"`

	Message     json.RawMessage   `json:"message,omitempty"`
	Messages    []json.RawMessage `json:"messages,omitempty"`
	ToolResults []json.RawMessage `json:"toolResults,omitempty"`

	AssistantMessageEvent *ai.AssistantMessageEvent `json:"assistantMessageEvent,omitempty"`

	ToolCallID    string          `json:"toolCallId,omitempty"`
	ToolName      string          `json:"toolName,omitempty"`
	Args          map[string]any  `json:"args,omitempty"`
	PartialResult *JSONToolResult `json:"partialResult,omitempty"`
	Result        *JSONToolResult `json:"result,omitempty"`
	IsError       bool            `json:"isError,omitempty"`
}

// JSONToolResult is the serializable form of an agent tool result.
type JSONToolResult struct {
	Content        []ai.ToolResultContent `json:"content"`
	Details        any                    `json:"details,omitempty"`
	Usage          *ai.Usage              `json:"usage,omitempty"`
	AddedToolNames []string               `json:"addedToolNames,omitempty"`
	Terminate      bool                   `json:"terminate,omitempty"`
}

func toJSONToolResult(result *agent.AgentToolResult) *JSONToolResult {
	if result == nil {
		return nil
	}
	return &JSONToolResult{
		Content:        result.Content,
		Details:        result.Details,
		Usage:          result.Usage,
		AddedToolNames: result.AddedToolNames,
		Terminate:      result.Terminate,
	}
}

// ToJSONEvent converts an agent event to its stdout wire form. Cumulative
// assistant snapshots are stripped from streaming deltas: message_start
// provides the initial message, deltas build it, and message_end provides the
// final authoritative message.
func ToJSONEvent(event agent.AgentEvent) JSONEvent {
	out := JSONEvent{Type: string(event.Type)}

	switch event.Type {
	case agent.EventAgentStart, agent.EventTurnStart:
		// No payload.

	case agent.EventAgentEnd:
		for _, message := range event.Messages {
			out.Messages = append(out.Messages, core.MessageJSON(message))
		}

	case agent.EventMessageStart, agent.EventMessageEnd:
		out.Message = core.MessageJSON(event.Message)

	case agent.EventMessageUpdate:
		if event.AssistantMessageEvent != nil {
			delta := *event.AssistantMessageEvent
			delta.Partial = nil
			out.AssistantMessageEvent = &delta
		}

	case agent.EventTurnEnd:
		out.Message = core.MessageJSON(event.Message)
		for _, result := range event.ToolResults {
			out.ToolResults = append(out.ToolResults, core.MessageJSON(ai.FromToolResult(result)))
		}

	case agent.EventToolExecutionStart, agent.EventToolExecutionUpdate, agent.EventToolExecutionEnd:
		out.ToolCallID = event.ToolCallID
		out.ToolName = event.ToolName
		out.Args = event.Args
		out.PartialResult = toJSONToolResult(event.PartialResult)
		out.Result = toJSONToolResult(event.Result)
		out.IsError = event.IsError
	}

	return out
}

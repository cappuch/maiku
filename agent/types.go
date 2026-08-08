// Package agent is a Go port of @earendil-works/pi-agent's low-level agent
// loop and stateful Agent wrapper. It works with AgentMessage (currently an
// alias for ai.Message) throughout, and transforms to []ai.Message only at
// the LLM call boundary.
package agent

import (
	"context"

	"github.com/mikus/maiku/ai"
)

// ThinkingLevel is the thinking/reasoning level for models that support it.
type ThinkingLevel string

const (
	ThinkingOff     ThinkingLevel = "off"
	ThinkingMinimal ThinkingLevel = "minimal"
	ThinkingLow     ThinkingLevel = "low"
	ThinkingMedium  ThinkingLevel = "medium"
	ThinkingHigh    ThinkingLevel = "high"
	ThinkingXHigh   ThinkingLevel = "xhigh"
	ThinkingMax     ThinkingLevel = "max"
)

// ToolExecutionMode controls how tool calls from a single assistant message
// are executed.
type ToolExecutionMode string

const (
	// ToolExecutionSequential executes each tool call one at a time.
	ToolExecutionSequential ToolExecutionMode = "sequential"
	// ToolExecutionParallel prepares tool calls sequentially, then executes
	// allowed tools concurrently.
	ToolExecutionParallel ToolExecutionMode = "parallel"
)

// QueueMode controls how many queued messages are drained at a queue point.
type QueueMode string

const (
	// QueueAll drains and injects every queued message at that point.
	QueueAll QueueMode = "all"
	// QueueOneAtATime drains and injects only the oldest queued message.
	QueueOneAtATime QueueMode = "one-at-a-time"
)

// AgentMessage is the transcript message type used throughout the agent
// loop. For this MVP it is an alias for ai.Message; custom app-defined
// message types (as supported via declaration merging in the TS version)
// are deferred to a future iteration.
type AgentMessage = ai.Message

// StreamFn is the stream function used by the agent loop. It matches
// ai.StreamFn exactly.
//
// Contract: must not error for request/model/runtime failures. It must
// return a valid *ai.AssistantMessageEventStream; failures should be encoded
// via protocol events and a final AssistantMessage with StopReason "error"
// or "aborted" plus ErrorMessage.
type StreamFn = ai.StreamFn

// AgentToolCall is a single tool-call block emitted by an assistant message.
type AgentToolCall = ai.ToolCall

// AgentToolResult is the final or partial result produced by a tool.
type AgentToolResult struct {
	// Content is the text/image content returned to the model.
	Content []ai.ToolResultContent
	// Details holds arbitrary structured details for logs or UI rendering.
	Details any
	// Usage is usage from the final tool execution itself, if available.
	// Not used for main LLM context accounting.
	Usage *ai.Usage
	// AddedToolNames lists names of tools introduced by this result and
	// available from this transcript point onward.
	AddedToolNames []string
	// Terminate hints that the agent should stop after the current tool
	// batch. Early termination only happens when every finalized tool
	// result in the batch sets this to true.
	Terminate bool
}

// AgentToolUpdateCallback streams partial execution updates from a tool.
// The callback is scoped to the current Execute invocation; calls made
// after Execute returns are ignored by the loop.
type AgentToolUpdateCallback func(partialResult AgentToolResult)

// AgentTool is a tool definition used by the agent runtime.
//
// Deviation from TS: TS parameterizes AgentTool<TParameters, TDetails> over
// a Typebox schema and a details type. Go's lack of ergonomic generics for
// this shape means arguments are plain map[string]any (validated against
// ai.Tool.Parameters JSON Schema) and Details is `any`.
type AgentTool struct {
	ai.Tool

	// Label is a human-readable label for UI display.
	Label string

	// PrepareArguments is an optional compatibility shim for raw tool-call
	// arguments before schema validation. Returning an error is equivalent
	// to the TS version throwing, and is converted to an error tool result.
	PrepareArguments func(args map[string]any) (map[string]any, error)

	// Execute runs the tool call. Returning an error is encoded as an error
	// tool result by the loop; it is never propagated as a fatal loop error.
	Execute func(ctx context.Context, toolCallID string, params map[string]any, onUpdate AgentToolUpdateCallback) (AgentToolResult, error)

	// ExecutionMode is a per-tool execution mode override. If empty, the
	// loop's default execution mode applies.
	ExecutionMode ToolExecutionMode
}

// AgentContext is a context snapshot passed into the low-level agent loop.
type AgentContext struct {
	// SystemPrompt is the system prompt included with the request.
	SystemPrompt string
	// Messages is the transcript visible to the model.
	Messages []AgentMessage
	// Tools are the tools available for this run.
	Tools []AgentTool
}

// BeforeToolCallResult is returned from AgentLoopConfig.BeforeToolCall.
//
// Setting Block prevents the tool from executing; the loop emits an error
// tool result instead, using Reason as the error text (falling back to a
// default message when empty). Terminate hints the agent should stop after
// the current tool batch; early termination only happens when every
// finalized tool result in the batch sets this to true.
type BeforeToolCallResult struct {
	Block     bool
	Reason    string
	Terminate bool
}

// AfterToolCallResult is a partial override returned from
// AgentLoopConfig.AfterToolCall.
//
// Merge semantics are field-by-field: a nil field keeps the original
// executed tool result value. There is no deep merge for Content or
// Details.
type AfterToolCallResult struct {
	// Content, if non-nil, replaces the tool result content array in full.
	Content []ai.ToolResultContent
	// Details, if non-nil, replaces the tool result details value in full.
	Details any
	// IsError, if non-nil, replaces the tool result error flag.
	IsError *bool
	// Usage, if non-nil, replaces the tool result usage.
	Usage *ai.Usage
	// Terminate, if non-nil, replaces the early-termination hint.
	Terminate *bool
}

// BeforeToolCallContext is passed to AgentLoopConfig.BeforeToolCall.
type BeforeToolCallContext struct {
	// AssistantMessage is the assistant message that requested the tool call.
	AssistantMessage ai.AssistantMessage
	// ToolCall is the raw tool-call block from AssistantMessage.Content.
	ToolCall AgentToolCall
	// Args are the validated tool arguments for the target tool schema.
	Args map[string]any
	// Context is the current agent context when the tool call is prepared.
	Context AgentContext
}

// AfterToolCallContext is passed to AgentLoopConfig.AfterToolCall.
type AfterToolCallContext struct {
	// AssistantMessage is the assistant message that requested the tool call.
	AssistantMessage ai.AssistantMessage
	// ToolCall is the raw tool-call block from AssistantMessage.Content.
	ToolCall AgentToolCall
	// Args are the validated tool arguments for the target tool schema.
	Args map[string]any
	// Result is the executed tool result before any AfterToolCall overrides.
	Result AgentToolResult
	// IsError is whether the executed tool result is currently an error.
	IsError bool
	// Context is the current agent context when the tool call is finalized.
	Context AgentContext
}

// ShouldStopAfterTurnContext is passed to AgentLoopConfig.ShouldStopAfterTurn
// and AgentLoopConfig.PrepareNextTurn (as PrepareNextTurnContext).
type ShouldStopAfterTurnContext struct {
	// Message is the assistant message that completed the turn.
	Message ai.AssistantMessage
	// ToolResults are the tool result messages from the preceding turn_end event.
	ToolResults []ai.ToolResultMessage
	// Context is the agent context after the turn's assistant message and
	// tool results have been appended.
	Context AgentContext
	// NewMessages are the messages this loop invocation will return if it
	// exits at this point.
	NewMessages []AgentMessage
}

// PrepareNextTurnContext is passed to AgentLoopConfig.PrepareNextTurn.
type PrepareNextTurnContext = ShouldStopAfterTurnContext

// AgentLoopTurnUpdate is replacement runtime state used by the agent loop
// before starting another provider request.
type AgentLoopTurnUpdate struct {
	// Context, if non-nil, replaces the context for the next provider request.
	Context *AgentContext
	// Model, if non-nil, replaces the model for the next provider request.
	Model *ai.Model
	// ThinkingLevel, if non-nil, replaces the thinking level for the next
	// provider request.
	ThinkingLevel *ThinkingLevel
}

// AgentLoopConfig configures a single agent loop run.
//
// Deviation from TS: every hook here takes a context.Context as its first
// argument (Go idiom) instead of TS's inconsistent per-hook AbortSignal
// parameter. This lets Agent pass hooks straight through without the
// signal-injection wrapping agent.ts needed for ShouldStopAfterTurn /
// PrepareNextTurn.
type AgentLoopConfig struct {
	ai.SimpleStreamOptions

	Model ai.Model

	// ConvertToLLM converts AgentMessage[] to LLM-compatible Message[]
	// before each LLM call. AgentMessages that cannot be converted should be
	// filtered out.
	//
	// Contract: must not error for expected/recoverable cases. Returning an
	// error interrupts the loop.
	ConvertToLLM func(messages []AgentMessage) ([]ai.Message, error)

	// TransformContext is an optional transform applied to the context
	// before ConvertToLLM. Use this for context-window management or
	// injecting context from external sources.
	TransformContext func(ctx context.Context, messages []AgentMessage) ([]AgentMessage, error)

	// GetAPIKey resolves an API key dynamically for each LLM call. Useful
	// for short-lived OAuth tokens that may expire during long-running tool
	// execution phases.
	GetAPIKey func(ctx context.Context, provider string) (string, error)

	// ShouldStopAfterTurn is called after each turn fully completes and
	// turn_end has been emitted. If it returns true, the loop emits
	// agent_end and exits before polling steering/follow-up queues.
	ShouldStopAfterTurn func(ctx context.Context, tctx ShouldStopAfterTurnContext) (bool, error)

	// PrepareNextTurn is called after turn_end and before the loop decides
	// whether another provider request should start.
	PrepareNextTurn func(ctx context.Context, tctx PrepareNextTurnContext) (*AgentLoopTurnUpdate, error)

	// GetSteeringMessages returns steering messages to inject mid-run.
	// Contract: must not error; return nil when no messages are available.
	GetSteeringMessages func(ctx context.Context) ([]AgentMessage, error)

	// GetFollowUpMessages returns follow-up messages to process after the
	// agent would otherwise stop.
	// Contract: must not error; return nil when no messages are available.
	GetFollowUpMessages func(ctx context.Context) ([]AgentMessage, error)

	// ToolExecution controls tool execution mode. Default: parallel.
	ToolExecution ToolExecutionMode

	// BeforeToolCall is called before a tool is executed, after arguments
	// have been validated. Errors and blocks are contained locally and
	// converted into an error tool result; they never interrupt the loop.
	BeforeToolCall func(ctx context.Context, bctx BeforeToolCallContext) (*BeforeToolCallResult, error)

	// AfterToolCall is called after a tool finishes executing, before
	// tool_execution_end and tool-result message events are emitted. Errors
	// are contained locally and converted into an error tool result.
	AfterToolCall func(ctx context.Context, actx AfterToolCallContext) (*AfterToolCallResult, error)
}

// AgentEventType discriminates AgentEvent values.
type AgentEventType string

const (
	EventAgentStart          AgentEventType = "agent_start"
	EventAgentEnd            AgentEventType = "agent_end"
	EventTurnStart           AgentEventType = "turn_start"
	EventTurnEnd             AgentEventType = "turn_end"
	EventMessageStart        AgentEventType = "message_start"
	EventMessageUpdate       AgentEventType = "message_update"
	EventMessageEnd          AgentEventType = "message_end"
	EventToolExecutionStart  AgentEventType = "tool_execution_start"
	EventToolExecutionUpdate AgentEventType = "tool_execution_update"
	EventToolExecutionEnd    AgentEventType = "tool_execution_end"
)

// AgentEvent is a single event emitted by the agent loop for UI updates.
//
// Deviation from TS: Go has no discriminated unions, so this is a single
// struct with fields populated according to Type (mirrors the existing
// ai.AssistantMessageEvent convention in this codebase).
type AgentEvent struct {
	Type AgentEventType

	// agent_end
	Messages []AgentMessage

	// turn_end, message_start, message_update, message_end
	Message AgentMessage

	// turn_end
	ToolResults []ai.ToolResultMessage

	// message_update
	AssistantMessageEvent *ai.AssistantMessageEvent

	// tool_execution_start, tool_execution_update, tool_execution_end
	ToolCallID    string
	ToolName      string
	Args          map[string]any
	PartialResult *AgentToolResult
	Result        *AgentToolResult
	IsError       bool
}

// AgentState is a snapshot of public agent state.
//
// Deviation from TS: the TS AgentState is a live interface with
// getter/setter accessors, so reading `agent.state.messages` always reflects
// current data. Go has no property accessors, so Agent.State() returns a
// point-in-time copy (Tools/Messages/PendingToolCalls are copied slices/maps)
// instead of a live view.
type AgentState struct {
	// SystemPrompt sent with each model request.
	SystemPrompt string
	// Model is the active model used for future turns.
	Model ai.Model
	// ThinkingLevel is the requested reasoning level for future turns.
	ThinkingLevel ThinkingLevel
	// Tools are the available tools.
	Tools []AgentTool
	// Messages is the conversation transcript.
	Messages []AgentMessage
	// IsStreaming is true while the agent is processing a prompt or
	// continuation. This remains true until awaited agent_end listeners
	// settle.
	IsStreaming bool
	// StreamingMessage is the partial assistant message for the current
	// streamed response, if any.
	StreamingMessage *AgentMessage
	// PendingToolCalls holds tool call ids currently executing.
	PendingToolCalls map[string]struct{}
	// ErrorMessage is the error from the most recent failed or aborted
	// assistant turn, if any.
	ErrorMessage string
}

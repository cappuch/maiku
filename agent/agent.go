package agent

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/mikus/maiku/ai"
)

func defaultConvertToLLM(messages []AgentMessage) ([]ai.Message, error) {
	out := make([]ai.Message, 0, len(messages))
	for _, m := range messages {
		if m.Role == "user" || m.Role == "assistant" || m.Role == "toolResult" {
			out = append(out, m)
		}
	}
	return out, nil
}

var defaultModel = ai.Model{
	ID:            "unknown",
	Name:          "unknown",
	API:           "unknown",
	Provider:      "unknown",
	BaseURL:       "",
	Reasoning:     false,
	Input:         []string{},
	Cost:          ai.ModelCost{},
	ContextWindow: 0,
	MaxTokens:     0,
}

// AgentInitialState seeds an Agent's mutable state at construction time.
type AgentInitialState struct {
	SystemPrompt  string
	Model         *ai.Model
	ThinkingLevel ThinkingLevel
	Tools         []AgentTool
	Messages      []AgentMessage
}

// AgentListener receives agent lifecycle events. It receives the active
// run's context (analogous to the TS AbortSignal parameter). Listener
// errors are awaited in subscription order and, like a rejected TS promise,
// interrupt the run (propagated back out of Prompt/Continue).
type AgentListener func(event AgentEvent, ctx context.Context) error

// AgentOptions configures a new Agent.
type AgentOptions struct {
	InitialState *AgentInitialState

	ConvertToLLM     func(messages []AgentMessage) ([]ai.Message, error)
	TransformContext func(ctx context.Context, messages []AgentMessage) ([]AgentMessage, error)
	StreamFn         StreamFn
	GetAPIKey        func(ctx context.Context, provider string) (string, error)

	OnPayload  func(payload any, model ai.Model) (any, error)
	OnResponse func(resp ai.ProviderResponse, model ai.Model)

	BeforeToolCall      func(ctx context.Context, bctx BeforeToolCallContext) (*BeforeToolCallResult, error)
	AfterToolCall       func(ctx context.Context, actx AfterToolCallContext) (*AfterToolCallResult, error)
	ShouldStopAfterTurn func(ctx context.Context, tctx ShouldStopAfterTurnContext) (bool, error)
	PrepareNextTurn     func(ctx context.Context, tctx PrepareNextTurnContext) (*AgentLoopTurnUpdate, error)

	SteeringMode QueueMode
	FollowUpMode QueueMode

	SessionID       string
	ThinkingBudgets *ai.ThinkingBudgets
	Transport       ai.Transport
	MaxRetryDelayMs *int
	ToolExecution   ToolExecutionMode
}

type pendingMessageQueue struct {
	mu       sync.Mutex
	mode     QueueMode
	messages []AgentMessage
}

func newPendingMessageQueue(mode QueueMode) *pendingMessageQueue {
	return &pendingMessageQueue{mode: mode}
}

func (q *pendingMessageQueue) Enqueue(message AgentMessage) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.messages = append(q.messages, message)
}

func (q *pendingMessageQueue) HasItems() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.messages) > 0
}

func (q *pendingMessageQueue) Drain() []AgentMessage {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.messages) == 0 {
		return nil
	}
	if q.mode == QueueAll {
		drained := q.messages
		q.messages = nil
		return drained
	}
	first := q.messages[0]
	q.messages = q.messages[1:]
	return []AgentMessage{first}
}

func (q *pendingMessageQueue) Clear() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.messages = nil
}

func (q *pendingMessageQueue) Mode() QueueMode {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.mode
}

func (q *pendingMessageQueue) SetMode(mode QueueMode) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.mode = mode
}

type activeRun struct {
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

type listenerEntry struct {
	id int
	fn AgentListener
}

// Agent is a stateful wrapper around the low-level agent loop. It owns the
// current transcript, emits lifecycle events, executes tools, and exposes
// queueing APIs for steering and follow-up messages.
//
// Deviation from TS: Agent uses context.Context for cancellation instead of
// AbortController/AbortSignal. Prompt/Continue accept a parent
// context.Context; Abort() cancels the derived run context.
type Agent struct {
	mu sync.Mutex

	systemPrompt     string
	model            ai.Model
	thinkingLevel    ThinkingLevel
	tools            []AgentTool
	messages         []AgentMessage
	isStreaming      bool
	streamingMessage *AgentMessage
	pendingToolCalls map[string]struct{}
	errorMessage     string

	listeners   []listenerEntry
	listenerSeq int

	steeringQueue *pendingMessageQueue
	followUpQueue *pendingMessageQueue

	activeRun *activeRun

	ConvertToLLM     func(messages []AgentMessage) ([]ai.Message, error)
	TransformContext func(ctx context.Context, messages []AgentMessage) ([]AgentMessage, error)
	StreamFunction   StreamFn
	GetAPIKey        func(ctx context.Context, provider string) (string, error)

	OnPayload  func(payload any, model ai.Model) (any, error)
	OnResponse func(resp ai.ProviderResponse, model ai.Model)

	BeforeToolCall      func(ctx context.Context, bctx BeforeToolCallContext) (*BeforeToolCallResult, error)
	AfterToolCall       func(ctx context.Context, actx AfterToolCallContext) (*AfterToolCallResult, error)
	ShouldStopAfterTurn func(ctx context.Context, tctx ShouldStopAfterTurnContext) (bool, error)
	PrepareNextTurn     func(ctx context.Context, tctx PrepareNextTurnContext) (*AgentLoopTurnUpdate, error)

	SessionID       string
	ThinkingBudgets *ai.ThinkingBudgets
	Transport       ai.Transport
	MaxRetryDelayMs *int
	ToolExecution   ToolExecutionMode
}

// NewAgent constructs an Agent from the given options.
func NewAgent(options AgentOptions) *Agent {
	a := &Agent{
		pendingToolCalls: map[string]struct{}{},
		thinkingLevel:    ThinkingOff,
		model:            defaultModel,

		steeringQueue: newPendingMessageQueue(orDefaultQueueMode(options.SteeringMode)),
		followUpQueue: newPendingMessageQueue(orDefaultQueueMode(options.FollowUpMode)),

		ConvertToLLM:     options.ConvertToLLM,
		TransformContext: options.TransformContext,
		StreamFunction:   options.StreamFn,
		GetAPIKey:        options.GetAPIKey,

		OnPayload:  options.OnPayload,
		OnResponse: options.OnResponse,

		BeforeToolCall:      options.BeforeToolCall,
		AfterToolCall:       options.AfterToolCall,
		ShouldStopAfterTurn: options.ShouldStopAfterTurn,
		PrepareNextTurn:     options.PrepareNextTurn,

		SessionID:       options.SessionID,
		ThinkingBudgets: options.ThinkingBudgets,
		Transport:       options.Transport,
		MaxRetryDelayMs: options.MaxRetryDelayMs,
		ToolExecution:   orDefaultToolExecution(options.ToolExecution),
	}

	if a.ConvertToLLM == nil {
		a.ConvertToLLM = defaultConvertToLLM
	}
	if a.StreamFunction == nil {
		if fn, err := GetDefaultStreamFn(); err == nil {
			a.StreamFunction = fn
		}
	}
	if a.Transport == "" {
		a.Transport = ai.TransportAuto
	}

	if options.InitialState != nil {
		is := options.InitialState
		a.systemPrompt = is.SystemPrompt
		if is.Model != nil {
			a.model = *is.Model
		}
		if is.ThinkingLevel != "" {
			a.thinkingLevel = is.ThinkingLevel
		}
		a.tools = append([]AgentTool{}, is.Tools...)
		a.messages = append([]AgentMessage{}, is.Messages...)
	}

	return a
}

func orDefaultQueueMode(mode QueueMode) QueueMode {
	if mode == "" {
		return QueueOneAtATime
	}
	return mode
}

func orDefaultToolExecution(mode ToolExecutionMode) ToolExecutionMode {
	if mode == "" {
		return ToolExecutionParallel
	}
	return mode
}

// Subscribe registers a listener for agent lifecycle events. It returns an
// unsubscribe function.
//
// Listener errors are awaited in subscription order and are included in the
// current run's settlement: an error from a listener interrupts the run and
// is returned from Prompt/Continue.
func (a *Agent) Subscribe(listener AgentListener) func() {
	a.mu.Lock()
	id := a.listenerSeq
	a.listenerSeq++
	a.listeners = append(a.listeners, listenerEntry{id: id, fn: listener})
	a.mu.Unlock()

	return func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		for i, l := range a.listeners {
			if l.id == id {
				a.listeners = append(a.listeners[:i:i], a.listeners[i+1:]...)
				break
			}
		}
	}
}

// State returns a point-in-time snapshot of the agent's public state. See
// the AgentState doc comment for how this differs from the live TS view.
func (a *Agent) State() AgentState {
	a.mu.Lock()
	defer a.mu.Unlock()

	pending := make(map[string]struct{}, len(a.pendingToolCalls))
	for k := range a.pendingToolCalls {
		pending[k] = struct{}{}
	}

	var streaming *AgentMessage
	if a.streamingMessage != nil {
		m := *a.streamingMessage
		streaming = &m
	}

	return AgentState{
		SystemPrompt:     a.systemPrompt,
		Model:            a.model,
		ThinkingLevel:    a.thinkingLevel,
		Tools:            append([]AgentTool{}, a.tools...),
		Messages:         append([]AgentMessage{}, a.messages...),
		IsStreaming:      a.isStreaming,
		StreamingMessage: streaming,
		PendingToolCalls: pending,
		ErrorMessage:     a.errorMessage,
	}
}

func (a *Agent) SetSystemPrompt(systemPrompt string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.systemPrompt = systemPrompt
}

func (a *Agent) SetModel(model ai.Model) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.model = model
}

func (a *Agent) SetThinkingLevel(level ThinkingLevel) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.thinkingLevel = level
}

// SetTools assigns the agent's tool set, copying the provided slice.
func (a *Agent) SetTools(tools []AgentTool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tools = append([]AgentTool{}, tools...)
}

// SetMessages assigns the agent's transcript, copying the provided slice.
func (a *Agent) SetMessages(messages []AgentMessage) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.messages = append([]AgentMessage{}, messages...)
}

// SteeringMode controls how queued steering messages are drained.
func (a *Agent) SteeringMode() QueueMode { return a.steeringQueue.Mode() }

// SetSteeringMode sets how queued steering messages are drained.
func (a *Agent) SetSteeringMode(mode QueueMode) { a.steeringQueue.SetMode(mode) }

// FollowUpMode controls how queued follow-up messages are drained.
func (a *Agent) FollowUpMode() QueueMode { return a.followUpQueue.Mode() }

// SetFollowUpMode sets how queued follow-up messages are drained.
func (a *Agent) SetFollowUpMode(mode QueueMode) { a.followUpQueue.SetMode(mode) }

// Steer queues a message to be injected after the current assistant turn
// finishes.
func (a *Agent) Steer(message AgentMessage) { a.steeringQueue.Enqueue(message) }

// FollowUp queues a message to run only after the agent would otherwise
// stop.
func (a *Agent) FollowUp(message AgentMessage) { a.followUpQueue.Enqueue(message) }

// ClearSteeringQueue removes all queued steering messages.
func (a *Agent) ClearSteeringQueue() { a.steeringQueue.Clear() }

// ClearFollowUpQueue removes all queued follow-up messages.
func (a *Agent) ClearFollowUpQueue() { a.followUpQueue.Clear() }

// ClearAllQueues removes all queued steering and follow-up messages.
func (a *Agent) ClearAllQueues() {
	a.ClearSteeringQueue()
	a.ClearFollowUpQueue()
}

// HasQueuedMessages returns true when either queue still contains pending
// messages.
func (a *Agent) HasQueuedMessages() bool {
	return a.steeringQueue.HasItems() || a.followUpQueue.HasItems()
}

// RunContext returns the active run's context, or nil if no run is active.
func (a *Agent) RunContext() context.Context {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.activeRun == nil {
		return nil
	}
	return a.activeRun.ctx
}

// Abort cancels the current run's context, if one is active.
func (a *Agent) Abort() {
	a.mu.Lock()
	run := a.activeRun
	a.mu.Unlock()
	if run != nil {
		run.cancel()
	}
}

// WaitForIdle blocks until the current run and all awaited event listeners
// have finished (i.e., after agent_end listeners settle). Returns
// immediately if no run is active.
func (a *Agent) WaitForIdle() {
	a.mu.Lock()
	run := a.activeRun
	a.mu.Unlock()
	if run == nil {
		return
	}
	<-run.done
}

// Reset clears transcript state, runtime state, and queued messages. It
// errors if a run is currently active.
func (a *Agent) Reset() error {
	a.mu.Lock()
	if a.activeRun != nil {
		a.mu.Unlock()
		return errors.New("agent is already processing. Wait for completion before resetting")
	}
	a.messages = nil
	a.isStreaming = false
	a.streamingMessage = nil
	a.pendingToolCalls = map[string]struct{}{}
	a.errorMessage = ""
	a.mu.Unlock()

	a.ClearFollowUpQueue()
	a.ClearSteeringQueue()
	return nil
}

// Prompt starts a new prompt from one or more messages. It errors if the
// agent is already processing a run.
func (a *Agent) Prompt(ctx context.Context, messages ...AgentMessage) error {
	a.mu.Lock()
	if a.activeRun != nil {
		a.mu.Unlock()
		return errors.New("agent is already processing a prompt. Use Steer() or FollowUp() to queue messages, or wait for completion")
	}
	a.mu.Unlock()
	return a.runPromptMessages(ctx, messages, false)
}

// PromptText starts a new prompt from plain text and optional images.
func (a *Agent) PromptText(ctx context.Context, text string, images ...ai.ImageContent) error {
	content := []any{ai.TextContent{Type: "text", Text: text}}
	for _, img := range images {
		content = append(content, img)
	}
	message := ai.Message{Role: "user", UserContent: content, Timestamp: time.Now().UnixMilli()}
	return a.Prompt(ctx, message)
}

// Continue continues from the current transcript. The last message must be
// a user or tool-result message, unless there are queued steering or
// follow-up messages while the last message is an assistant message.
func (a *Agent) Continue(ctx context.Context) error {
	a.mu.Lock()
	if a.activeRun != nil {
		a.mu.Unlock()
		return errors.New("agent is already processing. Wait for completion before continuing")
	}
	if len(a.messages) == 0 {
		a.mu.Unlock()
		return errors.New("no messages to continue from")
	}
	lastMessage := a.messages[len(a.messages)-1]
	a.mu.Unlock()

	if lastMessage.Role == "assistant" {
		if queuedSteering := a.steeringQueue.Drain(); len(queuedSteering) > 0 {
			return a.runPromptMessages(ctx, queuedSteering, true)
		}
		if queuedFollowUps := a.followUpQueue.Drain(); len(queuedFollowUps) > 0 {
			return a.runPromptMessages(ctx, queuedFollowUps, false)
		}
		return errors.New("cannot continue from message role: assistant")
	}

	return a.runContinuation(ctx)
}

func (a *Agent) runPromptMessages(ctx context.Context, messages []AgentMessage, skipInitialSteeringPoll bool) error {
	return a.runWithLifecycle(ctx, func(runCtx context.Context) error {
		_, err := RunAgentLoop(
			runCtx,
			messages,
			a.createContextSnapshot(),
			a.createLoopConfig(skipInitialSteeringPoll),
			a.processEvents,
			a.StreamFunction,
		)
		return err
	})
}

func (a *Agent) runContinuation(ctx context.Context) error {
	return a.runWithLifecycle(ctx, func(runCtx context.Context) error {
		_, err := RunAgentLoopContinue(
			runCtx,
			a.createContextSnapshot(),
			a.createLoopConfig(false),
			a.processEvents,
			a.StreamFunction,
		)
		return err
	})
}

func (a *Agent) createContextSnapshot() AgentContext {
	a.mu.Lock()
	defer a.mu.Unlock()
	return AgentContext{
		SystemPrompt: a.systemPrompt,
		Messages:     append([]AgentMessage{}, a.messages...),
		Tools:        append([]AgentTool{}, a.tools...),
	}
}

func (a *Agent) createLoopConfig(skipInitialSteeringPoll bool) AgentLoopConfig {
	a.mu.Lock()
	model := a.model
	thinkingLevel := a.thinkingLevel
	a.mu.Unlock()

	var reasoning ai.ThinkingLevel
	if thinkingLevel != ThinkingOff {
		reasoning = ai.ThinkingLevel(thinkingLevel)
	}

	var skipMu sync.Mutex
	skip := skipInitialSteeringPoll

	config := AgentLoopConfig{
		Model:               model,
		ConvertToLLM:        a.ConvertToLLM,
		TransformContext:    a.TransformContext,
		GetAPIKey:           a.GetAPIKey,
		ToolExecution:       a.ToolExecution,
		BeforeToolCall:      a.BeforeToolCall,
		AfterToolCall:       a.AfterToolCall,
		ShouldStopAfterTurn: a.ShouldStopAfterTurn,
		PrepareNextTurn:     a.PrepareNextTurn,
		GetSteeringMessages: func(ctx context.Context) ([]AgentMessage, error) {
			skipMu.Lock()
			if skip {
				skip = false
				skipMu.Unlock()
				return nil, nil
			}
			skipMu.Unlock()
			return a.steeringQueue.Drain(), nil
		},
		GetFollowUpMessages: func(ctx context.Context) ([]AgentMessage, error) {
			return a.followUpQueue.Drain(), nil
		},
	}

	config.Reasoning = reasoning
	config.SessionID = a.SessionID
	config.Transport = a.Transport
	config.ThinkingBudgets = a.ThinkingBudgets
	config.MaxRetryDelayMs = a.MaxRetryDelayMs
	config.OnPayload = a.OnPayload
	config.OnResponse = a.OnResponse

	return config
}

func (a *Agent) runWithLifecycle(ctx context.Context, executor func(runCtx context.Context) error) error {
	a.mu.Lock()
	if a.activeRun != nil {
		a.mu.Unlock()
		return errors.New("agent is already processing")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithCancel(ctx)
	run := &activeRun{ctx: runCtx, cancel: cancel, done: make(chan struct{})}
	a.activeRun = run
	a.isStreaming = true
	a.streamingMessage = nil
	a.errorMessage = ""
	a.mu.Unlock()

	runErr := executor(runCtx)
	if runErr != nil {
		aborted := runCtx.Err() != nil
		_ = a.handleRunFailure(run, runErr, aborted)
	}
	a.finishRun(run)
	close(run.done)

	return runErr
}

func (a *Agent) handleRunFailure(run *activeRun, runErr error, aborted bool) error {
	a.mu.Lock()
	model := a.model
	a.mu.Unlock()

	stopReason := ai.StopError
	if aborted {
		stopReason = ai.StopAborted
	}

	usage := ai.EmptyUsage()
	failure := ai.Message{
		Role:             "assistant",
		AssistantContent: []ai.AssistantContentBlock{{Type: "text", Text: ""}},
		API:              model.API,
		Provider:         model.Provider,
		Model:            model.ID,
		Usage:            &usage,
		StopReason:       stopReason,
		ErrorMessage:     runErr.Error(),
		Timestamp:        time.Now().UnixMilli(),
	}

	if err := a.processEventsWithRun(run, AgentEvent{Type: EventMessageStart, Message: failure}); err != nil {
		return err
	}
	if err := a.processEventsWithRun(run, AgentEvent{Type: EventMessageEnd, Message: failure}); err != nil {
		return err
	}
	if err := a.processEventsWithRun(run, AgentEvent{Type: EventTurnEnd, Message: failure, ToolResults: nil}); err != nil {
		return err
	}
	return a.processEventsWithRun(run, AgentEvent{Type: EventAgentEnd, Messages: []AgentMessage{failure}})
}

func (a *Agent) finishRun(run *activeRun) {
	a.mu.Lock()
	a.isStreaming = false
	a.streamingMessage = nil
	a.pendingToolCalls = map[string]struct{}{}
	if a.activeRun == run {
		a.activeRun = nil
	}
	a.mu.Unlock()
}

// processEvents reduces internal state for a loop event, then awaits
// listeners. It is passed to RunAgentLoop/RunAgentLoopContinue as the
// AgentEventSink.
//
// agent_end only means no further loop events will be emitted; the run is
// considered idle later, once finishRun() clears runtime-owned state.
func (a *Agent) processEvents(event AgentEvent) error {
	a.mu.Lock()
	run := a.activeRun
	a.mu.Unlock()
	if run == nil {
		return errors.New("agent listener invoked outside active run")
	}
	return a.processEventsWithRun(run, event)
}

func (a *Agent) processEventsWithRun(run *activeRun, event AgentEvent) error {
	a.mu.Lock()
	switch event.Type {
	case EventMessageStart, EventMessageUpdate:
		message := event.Message
		a.streamingMessage = &message

	case EventMessageEnd:
		a.streamingMessage = nil
		a.messages = append(a.messages, event.Message)

	case EventToolExecutionStart:
		if a.pendingToolCalls == nil {
			a.pendingToolCalls = map[string]struct{}{}
		}
		a.pendingToolCalls[event.ToolCallID] = struct{}{}

	case EventToolExecutionEnd:
		delete(a.pendingToolCalls, event.ToolCallID)

	case EventTurnEnd:
		if event.Message.Role == "assistant" && event.Message.ErrorMessage != "" {
			a.errorMessage = event.Message.ErrorMessage
		}

	case EventAgentEnd:
		a.streamingMessage = nil
	}

	listeners := append([]listenerEntry{}, a.listeners...)
	a.mu.Unlock()

	for _, l := range listeners {
		if err := l.fn(event, run.ctx); err != nil {
			return err
		}
	}
	return nil
}

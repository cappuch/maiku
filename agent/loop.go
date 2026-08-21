package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/mikus/maiku/ai"
)

type AgentEventSink func(event AgentEvent) error

func RunAgentLoop(
	ctx context.Context,
	prompts []AgentMessage,
	loopContext AgentContext,
	config AgentLoopConfig,
	emit AgentEventSink,
	streamFn StreamFn,
) ([]AgentMessage, error) {
	newMessages := append([]AgentMessage{}, prompts...)
	currentContext := AgentContext{
		SystemPrompt: loopContext.SystemPrompt,
		Messages:     append(append([]AgentMessage{}, loopContext.Messages...), prompts...),
		Tools:        loopContext.Tools,
	}

	safeEmit := syncEmit(emit)

	if err := safeEmit(AgentEvent{Type: EventAgentStart}); err != nil {
		return newMessages, err
	}
	if err := safeEmit(AgentEvent{Type: EventTurnStart}); err != nil {
		return newMessages, err
	}
	for _, prompt := range prompts {
		if err := safeEmit(AgentEvent{Type: EventMessageStart, Message: prompt}); err != nil {
			return newMessages, err
		}
		if err := safeEmit(AgentEvent{Type: EventMessageEnd, Message: prompt}); err != nil {
			return newMessages, err
		}
	}

	resolvedStreamFn, err := resolveStreamFn(streamFn)
	if err != nil {
		return newMessages, err
	}

	if err := runLoop(ctx, currentContext, &newMessages, config, safeEmit, resolvedStreamFn); err != nil {
		return newMessages, err
	}
	return newMessages, nil
}

// RunAgentLoopContinue continues an agent loop from the current context
// without adding a new message. Used for retries: context already has a
// user message or tool results.
//
// Important: the last message in context must convert to a user or
// toolResult message via config.ConvertToLLM. If it doesn't, the LLM
// provider will reject the request. This cannot be validated here since
// ConvertToLLM is only called once per turn.
func RunAgentLoopContinue(
	ctx context.Context,
	loopContext AgentContext,
	config AgentLoopConfig,
	emit AgentEventSink,
	streamFn StreamFn,
) ([]AgentMessage, error) {
	if len(loopContext.Messages) == 0 {
		return nil, errors.New("cannot continue: no messages in context")
	}
	if loopContext.Messages[len(loopContext.Messages)-1].Role == "assistant" {
		return nil, errors.New("cannot continue from message role: assistant")
	}

	newMessages := []AgentMessage{}
	currentContext := AgentContext{
		SystemPrompt: loopContext.SystemPrompt,
		Messages:     append([]AgentMessage{}, loopContext.Messages...),
		Tools:        loopContext.Tools,
	}

	safeEmit := syncEmit(emit)

	if err := safeEmit(AgentEvent{Type: EventAgentStart}); err != nil {
		return newMessages, err
	}
	if err := safeEmit(AgentEvent{Type: EventTurnStart}); err != nil {
		return newMessages, err
	}

	resolvedStreamFn, err := resolveStreamFn(streamFn)
	if err != nil {
		return newMessages, err
	}

	if err := runLoop(ctx, currentContext, &newMessages, config, safeEmit, resolvedStreamFn); err != nil {
		return newMessages, err
	}
	return newMessages, nil
}

func resolveStreamFn(streamFn StreamFn) (StreamFn, error) {
	if streamFn != nil {
		return streamFn, nil
	}
	return GetDefaultStreamFn()
}

// syncEmit serializes all calls to emit through a mutex. This is necessary
// because Go's parallel tool execution uses real goroutines (unlike JS's
// single-threaded cooperative concurrency), and downstream sinks such as
// Agent.processEvents mutate shared state.
func syncEmit(emit AgentEventSink) AgentEventSink {
	var mu sync.Mutex
	return func(event AgentEvent) error {
		mu.Lock()
		defer mu.Unlock()
		return emit(event)
	}
}

// runLoop is the main loop logic shared by RunAgentLoop and RunAgentLoopContinue.
func runLoop(
	ctx context.Context,
	initialContext AgentContext,
	newMessages *[]AgentMessage,
	initialConfig AgentLoopConfig,
	emit AgentEventSink,
	streamFn StreamFn,
) error {
	currentContext := initialContext
	config := initialConfig
	firstTurn := true

	pendingMessages, err := getSteeringMessages(ctx, config)
	if err != nil {
		return err
	}

	for {
		hasMoreToolCalls := true

		for hasMoreToolCalls || len(pendingMessages) > 0 {
			if !firstTurn {
				if err := emit(AgentEvent{Type: EventTurnStart}); err != nil {
					return err
				}
			} else {
				firstTurn = false
			}

			if len(pendingMessages) > 0 {
				for _, message := range pendingMessages {
					if err := emit(AgentEvent{Type: EventMessageStart, Message: message}); err != nil {
						return err
					}
					if err := emit(AgentEvent{Type: EventMessageEnd, Message: message}); err != nil {
						return err
					}
					currentContext.Messages = append(currentContext.Messages, message)
					*newMessages = append(*newMessages, message)
				}
			}

			assistantMessage, err := streamAssistantResponse(ctx, &currentContext, config, emit, streamFn)
			if err != nil {
				return err
			}
			*newMessages = append(*newMessages, ai.FromAssistant(assistantMessage))

			if assistantMessage.StopReason == ai.StopError || assistantMessage.StopReason == ai.StopAborted {
				if err := emit(AgentEvent{Type: EventTurnEnd, Message: ai.FromAssistant(assistantMessage), ToolResults: nil}); err != nil {
					return err
				}
				return emit(AgentEvent{Type: EventAgentEnd, Messages: *newMessages})
			}

			var toolCalls []AgentToolCall
			for _, c := range assistantMessage.Content {
				if tc, ok := c.AsToolCall(); ok {
					toolCalls = append(toolCalls, tc)
				}
			}

			var toolResults []ai.ToolResultMessage
			hasMoreToolCalls = false
			if len(toolCalls) > 0 {
				var batch executedToolCallBatch
				var err error
				if assistantMessage.StopReason == ai.StopLength {
					// A "length" stop means the output was cut off by the token
					// limit, so every tool call in the message may carry
					// truncated arguments. Fail them all instead of executing
					// potentially broken calls.
					//
					// This didn't work in Spettro but it works here, really odd.
					batch, err = failToolCallsFromTruncatedMessage(toolCalls, emit)
				} else {
					batch, err = executeToolCalls(ctx, &currentContext, assistantMessage, config, emit)
				}
				if err != nil {
					return err
				}
				toolResults = append(toolResults, batch.messages...)
				hasMoreToolCalls = !batch.terminate

				for _, result := range toolResults {
					resultMessage := ai.FromToolResult(result)
					currentContext.Messages = append(currentContext.Messages, resultMessage)
					*newMessages = append(*newMessages, resultMessage)
				}
			}

			if err := emit(AgentEvent{Type: EventTurnEnd, Message: ai.FromAssistant(assistantMessage), ToolResults: toolResults}); err != nil {
				return err
			}

			nextTurnContext := ShouldStopAfterTurnContext{
				Message:     assistantMessage,
				ToolResults: toolResults,
				Context:     currentContext,
				NewMessages: *newMessages,
			}

			if config.PrepareNextTurn != nil {
				update, err := config.PrepareNextTurn(ctx, nextTurnContext)
				if err != nil {
					return err
				}
				if update != nil {
					if update.Context != nil {
						currentContext = *update.Context
					}
					if update.Model != nil {
						config.Model = *update.Model
					}
					if update.ThinkingLevel != nil {
						if *update.ThinkingLevel == ThinkingOff {
							config.Reasoning = ""
						} else {
							config.Reasoning = ai.ThinkingLevel(*update.ThinkingLevel)
						}
					}
				}
			}

			if config.ShouldStopAfterTurn != nil {
				stop, err := config.ShouldStopAfterTurn(ctx, ShouldStopAfterTurnContext{
					Message:     assistantMessage,
					ToolResults: toolResults,
					Context:     currentContext,
					NewMessages: *newMessages,
				})
				if err != nil {
					return err
				}
				if stop {
					return emit(AgentEvent{Type: EventAgentEnd, Messages: *newMessages})
				}
			}

			pendingMessages, err = getSteeringMessages(ctx, config)
			if err != nil {
				return err
			}
		}

		followUpMessages, err := getFollowUpMessages(ctx, config)
		if err != nil {
			return err
		}
		if len(followUpMessages) > 0 {
			pendingMessages = followUpMessages
			continue
		}

		break
	}

	return emit(AgentEvent{Type: EventAgentEnd, Messages: *newMessages})
}

func getSteeringMessages(ctx context.Context, config AgentLoopConfig) ([]AgentMessage, error) {
	if config.GetSteeringMessages == nil {
		return nil, nil
	}
	return config.GetSteeringMessages(ctx)
}

func getFollowUpMessages(ctx context.Context, config AgentLoopConfig) ([]AgentMessage, error) {
	if config.GetFollowUpMessages == nil {
		return nil, nil
	}
	return config.GetFollowUpMessages(ctx)
}

// streamAssistantResponse streams an assistant response from the LLM. This
// is where []AgentMessage gets transformed to []ai.Message for the LLM.
func streamAssistantResponse(
	ctx context.Context,
	currentContext *AgentContext,
	config AgentLoopConfig,
	emit AgentEventSink,
	streamFn StreamFn,
) (ai.AssistantMessage, error) {
	messages := currentContext.Messages
	if config.TransformContext != nil {
		transformed, err := config.TransformContext(ctx, messages)
		if err != nil {
			return ai.AssistantMessage{}, err
		}
		messages = transformed
	}

	llmMessages, err := config.ConvertToLLM(messages)
	if err != nil {
		return ai.AssistantMessage{}, err
	}

	llmContext := ai.Context{
		SystemPrompt: currentContext.SystemPrompt,
		Messages:     llmMessages,
		Tools:        toAiTools(currentContext.Tools),
	}

	apiKey := config.APIKey
	if config.GetAPIKey != nil {
		resolved, err := config.GetAPIKey(ctx, config.Model.Provider)
		if err != nil {
			return ai.AssistantMessage{}, err
		}
		if resolved != "" {
			apiKey = resolved
		}
	}

	opts := config.SimpleStreamOptions
	opts.APIKey = apiKey
	if ctx != nil {
		opts.Signal = ctx.Done()
	}

	response := streamFn(config.Model, llmContext, &opts)

	var partialMessage *ai.AssistantMessage
	addedPartial := false

	for event := range response.Iter() {
		switch event.Type {
		case "start":
			partialMessage = event.Partial
			if partialMessage != nil {
				currentContext.Messages = append(currentContext.Messages, ai.FromAssistant(*partialMessage))
				addedPartial = true
				if err := emit(AgentEvent{Type: EventMessageStart, Message: ai.FromAssistant(*partialMessage)}); err != nil {
					return ai.AssistantMessage{}, err
				}
			}

		case "text_start", "text_delta", "text_end",
			"thinking_start", "thinking_delta", "thinking_end",
			"toolcall_start", "toolcall_delta", "toolcall_end":
			if partialMessage != nil {
				partialMessage = event.Partial
				if partialMessage != nil {
					currentContext.Messages[len(currentContext.Messages)-1] = ai.FromAssistant(*partialMessage)
					ev := event
					if err := emit(AgentEvent{
						Type:                  EventMessageUpdate,
						AssistantMessageEvent: &ev,
						Message:               ai.FromAssistant(*partialMessage),
					}); err != nil {
						return ai.AssistantMessage{}, err
					}
				}
			}

		case "done", "error":
			finalMessage := response.Result()
			if addedPartial {
				currentContext.Messages[len(currentContext.Messages)-1] = ai.FromAssistant(finalMessage)
			} else {
				currentContext.Messages = append(currentContext.Messages, ai.FromAssistant(finalMessage))
			}
			if !addedPartial {
				if err := emit(AgentEvent{Type: EventMessageStart, Message: ai.FromAssistant(finalMessage)}); err != nil {
					return ai.AssistantMessage{}, err
				}
			}
			if err := emit(AgentEvent{Type: EventMessageEnd, Message: ai.FromAssistant(finalMessage)}); err != nil {
				return ai.AssistantMessage{}, err
			}
			return finalMessage, nil
		}
	}

	finalMessage := response.Result()
	if addedPartial {
		currentContext.Messages[len(currentContext.Messages)-1] = ai.FromAssistant(finalMessage)
	} else {
		currentContext.Messages = append(currentContext.Messages, ai.FromAssistant(finalMessage))
		if err := emit(AgentEvent{Type: EventMessageStart, Message: ai.FromAssistant(finalMessage)}); err != nil {
			return ai.AssistantMessage{}, err
		}
	}
	if err := emit(AgentEvent{Type: EventMessageEnd, Message: ai.FromAssistant(finalMessage)}); err != nil {
		return ai.AssistantMessage{}, err
	}
	return finalMessage, nil
}

type executedToolCallBatch struct {
	messages  []ai.ToolResultMessage
	terminate bool
}

type preparedToolCall struct {
	toolCall AgentToolCall
	tool     AgentTool
	args     map[string]any
}

type immediateToolCallOutcome struct {
	result  AgentToolResult
	isError bool
}

type prepareOutcome struct {
	prepared  *preparedToolCall
	immediate *immediateToolCallOutcome
}

type executedToolCallOutcome struct {
	result  AgentToolResult
	isError bool
}

type finalizedToolCallOutcome struct {
	toolCall AgentToolCall
	result   AgentToolResult
	isError  bool
}

func shouldTerminateToolBatch(finalized []finalizedToolCallOutcome) bool {
	if len(finalized) == 0 {
		return false
	}
	for _, f := range finalized {
		if !f.result.Terminate {
			return false
		}
	}
	return true
}

// executeToolCalls executes tool calls from an assistant message, choosing
// sequential or parallel execution based on config and per-tool overrides.
func executeToolCalls(
	ctx context.Context,
	currentContext *AgentContext,
	assistantMessage ai.AssistantMessage,
	config AgentLoopConfig,
	emit AgentEventSink,
) (executedToolCallBatch, error) {
	var toolCalls []AgentToolCall
	for _, c := range assistantMessage.Content {
		if tc, ok := c.AsToolCall(); ok {
			toolCalls = append(toolCalls, tc)
		}
	}

	hasSequentialToolCall := false
	for _, tc := range toolCalls {
		if tool := findTool(currentContext.Tools, tc.Name); tool != nil && tool.ExecutionMode == ToolExecutionSequential {
			hasSequentialToolCall = true
			break
		}
	}

	if config.ToolExecution == ToolExecutionSequential || hasSequentialToolCall {
		return executeToolCallsSequential(ctx, currentContext, assistantMessage, toolCalls, config, emit)
	}
	return executeToolCallsParallel(ctx, currentContext, assistantMessage, toolCalls, config, emit)
}

func executeToolCallsSequential(
	ctx context.Context,
	currentContext *AgentContext,
	assistantMessage ai.AssistantMessage,
	toolCalls []AgentToolCall,
	config AgentLoopConfig,
	emit AgentEventSink,
) (executedToolCallBatch, error) {
	var finalizedCalls []finalizedToolCallOutcome
	var messages []ai.ToolResultMessage

	for _, toolCall := range toolCalls {
		if err := emit(AgentEvent{Type: EventToolExecutionStart, ToolCallID: toolCall.ID, ToolName: toolCall.Name, Args: toolCall.Arguments}); err != nil {
			return executedToolCallBatch{}, err
		}

		outcome := prepareToolCall(ctx, currentContext, assistantMessage, toolCall, config)
		var finalized finalizedToolCallOutcome
		if outcome.immediate != nil {
			finalized = finalizedToolCallOutcome{toolCall: toolCall, result: outcome.immediate.result, isError: outcome.immediate.isError}
		} else {
			executed, err := executePreparedToolCall(ctx, *outcome.prepared, emit)
			if err != nil {
				return executedToolCallBatch{}, err
			}
			finalized = finalizeExecutedToolCall(ctx, currentContext, assistantMessage, *outcome.prepared, executed, config)
		}

		if err := emitToolExecutionEnd(finalized, emit); err != nil {
			return executedToolCallBatch{}, err
		}
		toolResultMessage := createToolResultMessage(finalized)
		if err := emitToolResultMessage(toolResultMessage, emit); err != nil {
			return executedToolCallBatch{}, err
		}
		finalizedCalls = append(finalizedCalls, finalized)
		messages = append(messages, toolResultMessage)

		if isAborted(ctx) {
			break
		}
	}

	return executedToolCallBatch{messages: messages, terminate: shouldTerminateToolBatch(finalizedCalls)}, nil
}

type parallelEntry struct {
	immediate *finalizedToolCallOutcome
	run       func() (finalizedToolCallOutcome, error)
}

func executeToolCallsParallel(
	ctx context.Context,
	currentContext *AgentContext,
	assistantMessage ai.AssistantMessage,
	toolCalls []AgentToolCall,
	config AgentLoopConfig,
	emit AgentEventSink,
) (executedToolCallBatch, error) {
	var entries []parallelEntry

	for _, toolCall := range toolCalls {
		if err := emit(AgentEvent{Type: EventToolExecutionStart, ToolCallID: toolCall.ID, ToolName: toolCall.Name, Args: toolCall.Arguments}); err != nil {
			return executedToolCallBatch{}, err
		}

		outcome := prepareToolCall(ctx, currentContext, assistantMessage, toolCall, config)
		if outcome.immediate != nil {
			finalized := finalizedToolCallOutcome{toolCall: toolCall, result: outcome.immediate.result, isError: outcome.immediate.isError}
			if err := emitToolExecutionEnd(finalized, emit); err != nil {
				return executedToolCallBatch{}, err
			}
			entries = append(entries, parallelEntry{immediate: &finalized})
			if isAborted(ctx) {
				break
			}
			continue
		}

		prepared := *outcome.prepared
		entries = append(entries, parallelEntry{run: func() (finalizedToolCallOutcome, error) {
			executed, err := executePreparedToolCall(ctx, prepared, emit)
			if err != nil {
				return finalizedToolCallOutcome{}, err
			}
			finalized := finalizeExecutedToolCall(ctx, currentContext, assistantMessage, prepared, executed, config)
			if err := emitToolExecutionEnd(finalized, emit); err != nil {
				return finalizedToolCallOutcome{}, err
			}
			return finalized, nil
		}})
		if isAborted(ctx) {
			break
		}
	}

	results := make([]finalizedToolCallOutcome, len(entries))
	errs := make([]error, len(entries))
	var wg sync.WaitGroup
	for i, entry := range entries {
		if entry.immediate != nil {
			results[i] = *entry.immediate
			continue
		}
		wg.Add(1)
		go func(i int, run func() (finalizedToolCallOutcome, error)) {
			defer wg.Done()
			r, err := run()
			results[i] = r
			errs[i] = err
		}(i, entry.run)
	}
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			return executedToolCallBatch{}, err
		}
	}

	var messages []ai.ToolResultMessage
	for _, finalized := range results {
		toolResultMessage := createToolResultMessage(finalized)
		if err := emitToolResultMessage(toolResultMessage, emit); err != nil {
			return executedToolCallBatch{}, err
		}
		messages = append(messages, toolResultMessage)
	}

	return executedToolCallBatch{messages: messages, terminate: shouldTerminateToolBatch(results)}, nil
}

// failToolCallsFromTruncatedMessage fails all tool calls from an assistant
// message that was truncated by the output token limit. None of them are
// safe to execute; report each as an error so the model can re-issue them.
func failToolCallsFromTruncatedMessage(toolCalls []AgentToolCall, emit AgentEventSink) (executedToolCallBatch, error) {
	var messages []ai.ToolResultMessage
	for _, toolCall := range toolCalls {
		if err := emit(AgentEvent{Type: EventToolExecutionStart, ToolCallID: toolCall.ID, ToolName: toolCall.Name, Args: toolCall.Arguments}); err != nil {
			return executedToolCallBatch{}, err
		}
		finalized := finalizedToolCallOutcome{
			toolCall: toolCall,
			result: createErrorToolResult(fmt.Sprintf(
				"Tool call %q was not executed: the response hit the output token limit, so its arguments may be truncated. Re-issue the tool call with complete arguments.",
				toolCall.Name,
			)),
			isError: true,
		}
		if err := emitToolExecutionEnd(finalized, emit); err != nil {
			return executedToolCallBatch{}, err
		}
		toolResultMessage := createToolResultMessage(finalized)
		if err := emitToolResultMessage(toolResultMessage, emit); err != nil {
			return executedToolCallBatch{}, err
		}
		messages = append(messages, toolResultMessage)
	}
	return executedToolCallBatch{messages: messages, terminate: false}, nil
}

func prepareToolCallArguments(tool AgentTool, toolCall AgentToolCall) (AgentToolCall, error) {
	if tool.PrepareArguments == nil {
		return toolCall, nil
	}
	prepared, err := tool.PrepareArguments(toolCall.Arguments)
	if err != nil {
		return toolCall, err
	}
	toolCall.Arguments = prepared
	return toolCall, nil
}

// prepareToolCall validates and gates a tool call. Errors from
// PrepareArguments, schema validation, or BeforeToolCall are contained here
// and converted into an immediate error outcome; they never propagate as a
// fatal loop error (matches the TS try/catch around this logic).
func prepareToolCall(
	ctx context.Context,
	currentContext *AgentContext,
	assistantMessage ai.AssistantMessage,
	toolCall AgentToolCall,
	config AgentLoopConfig,
) prepareOutcome {
	tool := findTool(currentContext.Tools, toolCall.Name)
	if tool == nil {
		return prepareOutcome{immediate: &immediateToolCallOutcome{
			result:  createErrorToolResult(fmt.Sprintf("Tool %s not found", toolCall.Name)),
			isError: true,
		}}
	}

	preparedToolCallBlock, err := prepareToolCallArguments(*tool, toolCall)
	if err != nil {
		return prepareOutcome{immediate: &immediateToolCallOutcome{result: createErrorToolResult(err.Error()), isError: true}}
	}

	validatedArgs, err := ai.ValidateToolArguments(tool.Tool, preparedToolCallBlock.Arguments)
	if err != nil {
		return prepareOutcome{immediate: &immediateToolCallOutcome{result: createErrorToolResult(err.Error()), isError: true}}
	}

	if config.BeforeToolCall != nil {
		beforeResult, err := config.BeforeToolCall(ctx, BeforeToolCallContext{
			AssistantMessage: assistantMessage,
			ToolCall:         toolCall,
			Args:             validatedArgs,
			Context:          *currentContext,
		})
		if err != nil {
			return prepareOutcome{immediate: &immediateToolCallOutcome{result: createErrorToolResult(err.Error()), isError: true}}
		}
		if isAborted(ctx) {
			return prepareOutcome{immediate: &immediateToolCallOutcome{result: createErrorToolResult("Operation aborted"), isError: true}}
		}
		if beforeResult != nil && beforeResult.Block {
			reason := beforeResult.Reason
			if reason == "" {
				reason = "Tool execution was blocked"
			}
			result := createErrorToolResult(reason)
			if beforeResult.Terminate {
				result.Terminate = true
			}
			return prepareOutcome{immediate: &immediateToolCallOutcome{result: result, isError: true}}
		}
	}

	if isAborted(ctx) {
		return prepareOutcome{immediate: &immediateToolCallOutcome{result: createErrorToolResult("Operation aborted"), isError: true}}
	}

	return prepareOutcome{prepared: &preparedToolCall{toolCall: toolCall, tool: *tool, args: validatedArgs}}
}

// executePreparedToolCall runs the tool's Execute function. A returned error
// from Execute is contained and converted into an error result (matches the
// TS try/catch around tool.execute). Errors returned by emit for
// tool_execution_update events DO propagate, since they represent a failure
// of the host's event sink rather than the tool itself.
func executePreparedToolCall(ctx context.Context, prepared preparedToolCall, emit AgentEventSink) (executedToolCallOutcome, error) {
	acceptingUpdates := true
	var emitErr error

	onUpdate := func(partial AgentToolResult) {
		if !acceptingUpdates {
			return
		}
		if err := emit(AgentEvent{
			Type:          EventToolExecutionUpdate,
			ToolCallID:    prepared.toolCall.ID,
			ToolName:      prepared.toolCall.Name,
			Args:          prepared.toolCall.Arguments,
			PartialResult: &partial,
		}); err != nil && emitErr == nil {
			emitErr = err
		}
	}

	result, err := prepared.tool.Execute(ctx, prepared.toolCall.ID, prepared.args, onUpdate)
	acceptingUpdates = false

	if emitErr != nil {
		return executedToolCallOutcome{}, emitErr
	}
	if err != nil {
		return executedToolCallOutcome{result: createErrorToolResult(err.Error()), isError: true}, nil
	}
	return executedToolCallOutcome{result: result, isError: false}, nil
}

// finalizeExecutedToolCall applies AfterToolCall overrides. Errors from
// AfterToolCall are contained here and converted into an error result
// (matches the TS try/catch around config.afterToolCall).
func finalizeExecutedToolCall(
	ctx context.Context,
	currentContext *AgentContext,
	assistantMessage ai.AssistantMessage,
	prepared preparedToolCall,
	executed executedToolCallOutcome,
	config AgentLoopConfig,
) finalizedToolCallOutcome {
	result := executed.result
	isError := executed.isError

	if config.AfterToolCall != nil {
		afterResult, err := config.AfterToolCall(ctx, AfterToolCallContext{
			AssistantMessage: assistantMessage,
			ToolCall:         prepared.toolCall,
			Args:             prepared.args,
			Result:           result,
			IsError:          isError,
			Context:          *currentContext,
		})
		if err != nil {
			result = createErrorToolResult(err.Error())
			isError = true
		} else if afterResult != nil {
			if afterResult.Content != nil {
				result.Content = afterResult.Content
			}
			if afterResult.Details != nil {
				result.Details = afterResult.Details
			}
			if afterResult.Usage != nil {
				result.Usage = afterResult.Usage
			}
			if afterResult.Terminate != nil {
				result.Terminate = *afterResult.Terminate
			}
			if afterResult.IsError != nil {
				isError = *afterResult.IsError
			}
		}
	}

	return finalizedToolCallOutcome{toolCall: prepared.toolCall, result: result, isError: isError}
}

func createErrorToolResult(message string) AgentToolResult {
	return AgentToolResult{
		Content: []ai.ToolResultContent{{Type: "text", Text: message}},
		Details: map[string]any{},
	}
}

func emitToolExecutionEnd(finalized finalizedToolCallOutcome, emit AgentEventSink) error {
	result := finalized.result
	return emit(AgentEvent{
		Type:       EventToolExecutionEnd,
		ToolCallID: finalized.toolCall.ID,
		ToolName:   finalized.toolCall.Name,
		Result:     &result,
		IsError:    finalized.isError,
	})
}

func createToolResultMessage(finalized finalizedToolCallOutcome) ai.ToolResultMessage {
	content := finalized.result.Content
	if content == nil {
		// Untyped tools can return results without content; normalize so a
		// nil slice never enters session history or provider payloads.
		content = []ai.ToolResultContent{}
	}
	return ai.ToolResultMessage{
		Role:           "toolResult",
		ToolCallID:     finalized.toolCall.ID,
		ToolName:       finalized.toolCall.Name,
		Content:        content,
		Details:        finalized.result.Details,
		Usage:          finalized.result.Usage,
		AddedToolNames: finalized.result.AddedToolNames,
		IsError:        finalized.isError,
		Timestamp:      time.Now().UnixMilli(),
	}
}

func emitToolResultMessage(toolResultMessage ai.ToolResultMessage, emit AgentEventSink) error {
	message := ai.FromToolResult(toolResultMessage)
	if err := emit(AgentEvent{Type: EventMessageStart, Message: message}); err != nil {
		return err
	}
	return emit(AgentEvent{Type: EventMessageEnd, Message: message})
}

func findTool(tools []AgentTool, name string) *AgentTool {
	for i := range tools {
		if tools[i].Name == name {
			return &tools[i]
		}
	}
	return nil
}

func toAiTools(tools []AgentTool) []ai.Tool {
	if tools == nil {
		return nil
	}
	out := make([]ai.Tool, len(tools))
	for i, t := range tools {
		out[i] = t.Tool
	}
	return out
}

func isAborted(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

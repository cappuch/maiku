package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mikus/maiku/agent"
	"github.com/mikus/maiku/ai"
	"github.com/mikus/maiku/ai/auth"
	"github.com/mikus/maiku/codingagent/core/compaction"
)

// DefaultThinkingLevel matches the TypeScript default.
const DefaultThinkingLevel = agent.ThinkingMedium

// ConvertToLLM projects transcript messages into the LLM-visible message
// list. Assistant turns with no text or tool calls (thinking-only length
// stops, empty error/abort shells) are dropped so providers that reject
// `{"role":"assistant"}` without content do not 400 on the next turn.
func ConvertToLLM(messages []agent.AgentMessage) ([]ai.Message, error) {
	out := make([]ai.Message, 0, len(messages))
	for _, m := range messages {
		switch m.Role {
		case "user", "toolResult":
			out = append(out, m)
		case "assistant":
			if !assistantHasLLMContent(m) {
				continue
			}
			out = append(out, m)
		}
	}
	return out, nil
}

func assistantHasLLMContent(m agent.AgentMessage) bool {
	for _, c := range m.AssistantContent {
		switch c.Type {
		case "text":
			if strings.TrimSpace(c.Text) != "" {
				return true
			}
		case "toolCall":
			return true
		}
	}
	return false
}

// AgentSessionOptions configures NewAgentSession.
type AgentSessionOptions struct {
	Model         ai.Model
	ThinkingLevel agent.ThinkingLevel
	SystemPrompt  string
	Tools         []agent.AgentTool
	// APIKey overrides the provider environment variable when set.
	APIKey string
	// StreamFn overrides the provider stream runtime. It defaults to
	// ai.StreamSimple and is used by root-owned subagents to inherit custom
	// host runtimes without changing the agent loop.
	StreamFn agent.StreamFn
	// Sessions persists the transcript. May be nil for ephemeral runs.
	Sessions *SessionManager
	// Compaction controls automatic context compaction after each prompt.
	// A zero value disables it.
	Compaction compaction.Settings
	// OnCompaction is notified after a successful compaction.
	OnCompaction func(compaction.Result)
	// OnCompactionError is notified when compaction fails. Failure is not
	// fatal: the turn runs with the full transcript.
	OnCompactionError func(error)
	// Retry controls automatic retries of transient provider errors.
	Retry ai.RetryPolicy
	// OnRetry is notified before each retry backoff sleep.
	OnRetry func(attempt, maxAttempts, delayMs int, errorMessage string)
}

// AgentSession is a thin wrapper around agent.Agent that adds session
// persistence and a few conveniences used by the CLI modes.
type AgentSession struct {
	agent    *agent.Agent
	sessions *SessionManager
	model    ai.Model
	apiKey   string

	compaction        compaction.Settings
	onCompaction      func(compaction.Result)
	onCompactionError func(error)
	retry             ai.RetryPolicy
	onRetry           func(attempt, maxAttempts, delayMs int, errorMessage string)

	mu           sync.Mutex
	unsubPersist func()
	disposed     bool
}

// NewAgentSession builds an agent wired to StreamSimple, the built-in tools,
// and (optionally) a session file.
func NewAgentSession(options AgentSessionOptions) *AgentSession {
	thinkingLevel := options.ThinkingLevel
	if thinkingLevel == "" {
		thinkingLevel = DefaultThinkingLevel
	}
	if !options.Model.Reasoning {
		switch {
		case thinkingLevel == agent.ThinkingOff:
			// Explicit off (or a non-reasoning model's default) — nothing to do.
		case thinkingLevel == DefaultThinkingLevel:
			// No explicit choice and the catalog says this model doesn't
			// reason — keep thinking off by default.
			thinkingLevel = agent.ThinkingOff
		default:
			// An explicit non-default level opts the model into reasoning even
			// when the /models catalog doesn't advertise it (e.g. DeepSeek V4
			// on OpenCode), so the level actually reaches the API.
			options.Model.Reasoning = true
		}
	}

	var initialMessages []ai.Message
	sessionID := ""
	if options.Sessions != nil {
		initialMessages = options.Sessions.Messages()
		sessionID = options.Sessions.Header().ID
	}

	model := options.Model
	apiKey := options.APIKey
	streamFn := options.StreamFn
	if streamFn == nil {
		streamFn = ai.StreamSimple
	}

	a := agent.NewAgent(agent.AgentOptions{
		InitialState: &agent.AgentInitialState{
			SystemPrompt:  options.SystemPrompt,
			Model:         &model,
			ThinkingLevel: thinkingLevel,
			Tools:         options.Tools,
			Messages:      initialMessages,
		},
		ConvertToLLM: ConvertToLLM,
		StreamFn:     streamFn,
		GetAPIKey: func(ctx context.Context, provider string) (string, error) {
			if apiKey != "" {
				return apiKey, nil
			}
			if key := auth.ResolveAPIKey(provider); key != "" {
				return key, nil
			}
			return "", fmt.Errorf("no API key for provider %q; pass --api-key or set the provider environment variable", provider)
		},
		SessionID: sessionID,
	})

	s := &AgentSession{
		agent:             a,
		sessions:          options.Sessions,
		model:             model,
		apiKey:            apiKey,
		compaction:        options.Compaction,
		onCompaction:      options.OnCompaction,
		onCompactionError: options.OnCompactionError,
		retry:             options.Retry,
		onRetry:           options.OnRetry,
	}

	if options.Sessions != nil {
		// Persist as messages finalize so a crash mid-run still leaves a
		// usable session file for --continue.
		s.unsubPersist = a.Subscribe(func(event agent.AgentEvent, ctx context.Context) error {
			if event.Type != agent.EventMessageEnd {
				return nil
			}
			switch event.Message.Role {
			case "user", "assistant", "toolResult":
				if err := options.Sessions.AppendMessage(event.Message); err != nil {
					return err
				}
			}
			return nil
		})
	}

	return s
}

// Agent exposes the underlying agent for advanced use.
func (s *AgentSession) Agent() *agent.Agent { return s.agent }

// Model returns the model this session runs with.
func (s *AgentSession) Model() ai.Model { return s.model }

// SessionManager returns the session manager, or nil for ephemeral sessions.
func (s *AgentSession) SessionManager() *SessionManager { return s.sessions }

// Subscribe registers an event listener and returns an unsubscribe function.
func (s *AgentSession) Subscribe(listener func(event agent.AgentEvent)) func() {
	return s.agent.Subscribe(func(event agent.AgentEvent, ctx context.Context) error {
		listener(event)
		return nil
	})
}

// Prompt sends a user message and runs the agent loop until it stops. When
// compaction is enabled and the transcript has outgrown the model's context
// window, the older history is summarized first so the turn starts with room
// to work. A failed summarization is reported through OnCompactionError and
// the turn proceeds uncompacted. Transient provider errors are retried with
// exponential backoff when Retry is enabled.
func (s *AgentSession) Prompt(ctx context.Context, text string, images ...ai.ImageContent) error {
	if err := s.MaybeCompact(ctx); err != nil {
		if s.onCompactionError != nil {
			s.onCompactionError(err)
		}
	}

	content := text
	if len(images) > 0 {
		parts := []any{ai.TextContent{Type: "text", Text: text}}
		for _, image := range images {
			parts = append(parts, image)
		}
		message := ai.Message{Role: "user", UserContent: parts, Timestamp: time.Now().UnixMilli()}
		if err := s.agent.Prompt(ctx, message); err != nil {
			return err
		}
		return s.maybeRetry(ctx)
	}
	message := ai.Message{Role: "user", UserContent: content, Timestamp: time.Now().UnixMilli()}
	if err := s.agent.Prompt(ctx, message); err != nil {
		return err
	}
	return s.maybeRetry(ctx)
}

// maybeRetry restarts the agent on transient provider errors using Continue.
func (s *AgentSession) maybeRetry(ctx context.Context) error {
	policy := s.retry
	if !policy.Enabled || policy.MaxRetries <= 0 {
		return nil
	}
	baseDelay := policy.BaseDelayMs
	if baseDelay <= 0 {
		baseDelay = 2000
	}

	for attempt := 1; attempt <= policy.MaxRetries; attempt++ {
		assistant, ok := s.LastAssistantMessage()
		if !ok || !ai.IsRetryableAssistantError(assistant) {
			return nil
		}

		delayMs := baseDelay * (1 << (attempt - 1))
		if s.onRetry != nil {
			s.onRetry(attempt, policy.MaxRetries, delayMs, assistant.ErrorMessage)
		}

		timer := time.NewTimer(time.Duration(delayMs) * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}

		// Drop the failed assistant turn so Continue resumes from the prior
		// user/toolResult message, matching the TypeScript AgentSession.
		messages := s.agent.State().Messages
		if len(messages) > 0 && messages[len(messages)-1].Role == "assistant" {
			s.agent.SetMessages(messages[:len(messages)-1])
		}
		if err := s.agent.Continue(ctx); err != nil {
			return err
		}
	}
	return nil
}

// ShouldCompact reports whether the transcript has grown past the compaction
// threshold for this model.
func (s *AgentSession) ShouldCompact() bool {
	if !s.compaction.Enabled {
		return false
	}
	messages := s.agent.State().Messages
	estimate := compaction.EstimateContextTokens(messages)
	return compaction.ShouldCompact(estimate.Tokens, s.model.ContextWindow, s.compaction)
}

// MaybeCompact compacts the transcript when it is over the threshold. A
// failed summarization is not fatal: the run continues with the full
// transcript and the error is returned for the caller to report.
func (s *AgentSession) MaybeCompact(ctx context.Context) error {
	if !s.ShouldCompact() {
		return nil
	}

	_, err := s.Compact(ctx)
	if errors.Is(err, compaction.ErrNothingToCompact) {
		return nil
	}
	return err
}

// Compact immediately summarizes older conversation history, regardless of
// the automatic compaction threshold. It returns ErrNothingToCompact when the
// configured recent-history budget already covers the whole transcript.
func (s *AgentSession) Compact(ctx context.Context) (compaction.Result, error) {
	state := s.agent.State()
	messages := state.Messages
	result, err := compaction.Compact(ctx, compaction.Options{
		Messages:        messages,
		Model:           s.model,
		Settings:        s.compaction,
		APIKey:          s.apiKey,
		ThinkingLevel:   state.ThinkingLevel,
		PreviousSummary: compaction.ExtractPreviousSummary(messages),
	})
	if err != nil {
		return compaction.Result{}, err
	}

	s.agent.SetMessages(result.Messages)
	if s.onCompaction != nil {
		s.onCompaction(result)
	}
	return result, nil
}

// State returns a snapshot of the agent state.
func (s *AgentSession) State() agent.AgentState { return s.agent.State() }

// LastAssistantMessage returns the most recent assistant message, if any.
func (s *AgentSession) LastAssistantMessage() (ai.AssistantMessage, bool) {
	messages := s.agent.State().Messages
	for i := len(messages) - 1; i >= 0; i-- {
		if assistant, ok := messages[i].AsAssistant(); ok {
			return assistant, true
		}
	}
	return ai.AssistantMessage{}, false
}

// LastAssistantText returns the concatenated text of the most recent
// assistant message.
func (s *AgentSession) LastAssistantText() string {
	if assistant, ok := s.LastAssistantMessage(); ok {
		return ai.AssistantText(assistant)
	}
	return ""
}

// Abort cancels an in-flight run.
func (s *AgentSession) Abort() { s.agent.Abort() }

// WaitForIdle blocks until the active run finishes.
func (s *AgentSession) WaitForIdle() { s.agent.WaitForIdle() }

// Dispose stops persistence listeners. It is safe to call more than once.
func (s *AgentSession) Dispose() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.disposed {
		return
	}
	s.disposed = true
	if s.unsubPersist != nil {
		s.unsubPersist()
		s.unsubPersist = nil
	}
}

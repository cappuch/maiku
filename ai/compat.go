package ai

import (
	"fmt"
	"time"

	"github.com/mikus/maiku/ai/auth"
)

// StreamSimple dispatches to the streaming implementation registered for
// model.API (see RegisterAPI). It never panics: if no implementation is
// registered for the model's API, or the model has no API set, it returns a
// stream that immediately emits an "error" event.
//
// Callers typically get a registered implementation by importing
// github.com/mikus/maiku/ai/api/anthropic and
// github.com/mikus/maiku/ai/api/openaicompletions (or ai/providers, which
// imports both) for their side-effecting init() registration.
func StreamSimple(model Model, ctx Context, opts *SimpleStreamOptions) *AssistantMessageEventStream {
	opts = withEnvAPIKey(model, opts)
	fn, ok := LookupAPI(model.API)
	if !ok {
		s := NewAssistantMessageEventStream()
		errMsg := AssistantMessage{
			Role:         "assistant",
			Content:      []AssistantContentBlock{},
			API:          model.API,
			Provider:     model.Provider,
			Model:        model.ID,
			Usage:        EmptyUsage(),
			StopReason:   StopError,
			ErrorMessage: fmt.Sprintf("no streaming implementation registered for api %q (model %s)", model.API, model.ID),
			Timestamp:    time.Now().UnixMilli(),
		}
		s.Push(AssistantMessageEvent{Type: "error", Reason: StopError, Error: &errMsg})
		return s
	}
	return fn(model, ctx, opts)
}

// CompleteSimple drains StreamSimple to the final AssistantMessage. It is a
// non-streaming convenience wrapper; the returned message always has a
// terminal stop reason (never "pending").
func CompleteSimple(model Model, ctx Context, opts *SimpleStreamOptions) AssistantMessage {
	s := StreamSimple(model, ctx, opts)
	return s.Result()
}

// withEnvAPIKey fills opts.APIKey from a provider environment variable when
// the caller did not already set one explicitly, mirroring pi-ai's
// withEnvApiKey. Never mutates the caller's opts.
func withEnvAPIKey(model Model, opts *SimpleStreamOptions) *SimpleStreamOptions {
	if opts != nil && opts.APIKey != "" {
		return opts
	}
	apiKey := auth.ResolveAPIKey(model.Provider)
	if apiKey == "" {
		if opts == nil {
			return &SimpleStreamOptions{}
		}
		return opts
	}
	next := SimpleStreamOptions{}
	if opts != nil {
		next = *opts
	}
	next.APIKey = apiKey
	return &next
}

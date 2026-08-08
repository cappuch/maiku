package ai

import "sync"

// EventStream is a push-based async event queue, mirroring pi-ai EventStream.
type EventStream[T any, R any] struct {
	mu            sync.Mutex
	queue         []T
	waiting       []chan iteratorResult[T]
	done          bool
	resultCh      chan R
	gotResult     bool
	isComplete    func(T) bool
	extractResult func(T) R
}

type iteratorResult[T any] struct {
	value T
	ok    bool
}

func NewEventStream[T any, R any](isComplete func(T) bool, extractResult func(T) R) *EventStream[T, R] {
	return &EventStream[T, R]{
		resultCh:      make(chan R, 1),
		isComplete:    isComplete,
		extractResult: extractResult,
	}
}

func (s *EventStream[T, R]) Push(event T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return
	}
	if s.isComplete(event) {
		s.done = true
		if !s.gotResult {
			s.gotResult = true
			s.resultCh <- s.extractResult(event)
		}
	}
	if len(s.waiting) > 0 {
		w := s.waiting[0]
		s.waiting = s.waiting[1:]
		w <- iteratorResult[T]{value: event, ok: true}
		return
	}
	s.queue = append(s.queue, event)
}

func (s *EventStream[T, R]) End(result *R) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.done = true
	if result != nil && !s.gotResult {
		s.gotResult = true
		s.resultCh <- *result
	}
	for _, w := range s.waiting {
		w <- iteratorResult[T]{ok: false}
	}
	s.waiting = nil
}

// Next returns the next event. ok=false means the stream ended.
func (s *EventStream[T, R]) Next() (T, bool) {
	s.mu.Lock()
	if len(s.queue) > 0 {
		ev := s.queue[0]
		s.queue = s.queue[1:]
		s.mu.Unlock()
		return ev, true
	}
	if s.done {
		s.mu.Unlock()
		var zero T
		return zero, false
	}
	ch := make(chan iteratorResult[T], 1)
	s.waiting = append(s.waiting, ch)
	s.mu.Unlock()
	res := <-ch
	return res.value, res.ok
}

// Iter returns a channel of events (closes when done).
func (s *EventStream[T, R]) Iter() <-chan T {
	ch := make(chan T)
	go func() {
		defer close(ch)
		for {
			ev, ok := s.Next()
			if !ok {
				return
			}
			ch <- ev
		}
	}()
	return ch
}

func (s *EventStream[T, R]) Result() R {
	return <-s.resultCh
}

// AssistantMessageEventStream terminates on done/error.
type AssistantMessageEventStream = EventStream[AssistantMessageEvent, AssistantMessage]

func NewAssistantMessageEventStream() *AssistantMessageEventStream {
	return NewEventStream(
		func(e AssistantMessageEvent) bool {
			return e.Type == "done" || e.Type == "error"
		},
		func(e AssistantMessageEvent) AssistantMessage {
			if e.Type == "done" && e.Message != nil {
				return *e.Message
			}
			if e.Type == "error" && e.Error != nil {
				return *e.Error
			}
			return AssistantMessage{StopReason: StopError, ErrorMessage: "unexpected stream termination"}
		},
	)
}

// StreamFn is the provider-agnostic streaming contract used by the agent loop.
type StreamFn func(model Model, ctx Context, opts *SimpleStreamOptions) *AssistantMessageEventStream

// apiRegistry maps an API identifier (e.g. "anthropic-messages") to its
// streaming implementation. API packages (ai/api/anthropic, ai/api/openaicompletions,
// ...) register themselves via RegisterAPI in an init() function, avoiding an
// import cycle between the root ai package and its api subpackages.
var apiRegistry = map[string]StreamFn{}

// RegisterAPI registers a streaming implementation for the given API identifier.
// Intended to be called from an API package's init() function.
func RegisterAPI(api string, fn StreamFn) {
	apiRegistry[api] = fn
}

// LookupAPI returns the registered streaming implementation for an API identifier, if any.
func LookupAPI(api string) (StreamFn, bool) {
	fn, ok := apiRegistry[api]
	return fn, ok
}

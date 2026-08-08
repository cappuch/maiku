package compaction

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/mikus/maiku/agent"
	"github.com/mikus/maiku/ai"
)

const (
	DefaultReserveTokens    = 16384
	DefaultKeepRecentTokens = 20000
	// MinKeepRecentTokens is the floor used when a small context window
	// cannot fit the configured recent-history budget.
	MinKeepRecentTokens = 2048
	// EstimatedImageChars is the character weight assigned to an image block.
	EstimatedImageChars = 4800
)

// ErrNothingToCompact is returned when no history can be summarized away,
// which happens when the recent-history budget already covers the whole
// transcript.
var ErrNothingToCompact = errors.New("compaction: nothing to compact")

// Settings controls when compaction triggers and how much history it keeps.
type Settings struct {
	Enabled          bool
	ReserveTokens    int
	KeepRecentTokens int
}

// DefaultSettings returns the built-in compaction settings.
func DefaultSettings() Settings {
	return Settings{
		Enabled:          true,
		ReserveTokens:    DefaultReserveTokens,
		KeepRecentTokens: DefaultKeepRecentTokens,
	}
}

// WithDefaults fills unset (non-positive) numeric fields from the defaults.
func (s Settings) WithDefaults() Settings {
	if s.ReserveTokens <= 0 {
		s.ReserveTokens = DefaultReserveTokens
	}
	if s.KeepRecentTokens <= 0 {
		s.KeepRecentTokens = DefaultKeepRecentTokens
	}
	return s
}

// CalculateContextTokens reads the total context size from a usage record,
// falling back to the component sum when the provider omits the total.
func CalculateContextTokens(usage ai.Usage) int {
	if usage.TotalTokens > 0 {
		return usage.TotalTokens
	}
	return usage.Input + usage.Output + usage.CacheRead + usage.CacheWrite
}

// assistantUsage returns the usage of a successful assistant message.
// Aborted, errored, and all-zero turns carry no usable accounting.
func assistantUsage(message agent.AgentMessage) (ai.Usage, bool) {
	if message.Role != "assistant" || message.Usage == nil {
		return ai.Usage{}, false
	}
	if message.StopReason == ai.StopAborted || message.StopReason == ai.StopError {
		return ai.Usage{}, false
	}
	if CalculateContextTokens(*message.Usage) <= 0 {
		return ai.Usage{}, false
	}
	return *message.Usage, true
}

// ContextUsageEstimate describes how large the transcript currently is.
type ContextUsageEstimate struct {
	// Tokens is the best estimate of the whole context.
	Tokens int
	// UsageTokens comes from the last assistant message's reported usage.
	UsageTokens int
	// TrailingTokens is the estimate for messages after that usage.
	TrailingTokens int
	// LastUsageIndex is the index of that assistant message, or -1.
	LastUsageIndex int
}

// EstimateContextTokens estimates the context size, anchoring on the most
// recent provider-reported usage when one exists.
func EstimateContextTokens(messages []agent.AgentMessage) ContextUsageEstimate {
	lastIndex := -1
	var lastUsage ai.Usage
	for i := len(messages) - 1; i >= 0; i-- {
		if usage, ok := assistantUsage(messages[i]); ok {
			lastUsage, lastIndex = usage, i
			break
		}
	}

	if lastIndex == -1 {
		estimated := 0
		for _, message := range messages {
			estimated += EstimateTokens(message)
		}
		return ContextUsageEstimate{Tokens: estimated, TrailingTokens: estimated, LastUsageIndex: -1}
	}

	usageTokens := CalculateContextTokens(lastUsage)
	trailing := 0
	for i := lastIndex + 1; i < len(messages); i++ {
		trailing += EstimateTokens(messages[i])
	}
	return ContextUsageEstimate{
		Tokens:         usageTokens + trailing,
		UsageTokens:    usageTokens,
		TrailingTokens: trailing,
		LastUsageIndex: lastIndex,
	}
}

// ShouldCompact reports whether the context has grown past the point where
// the reserved headroom would be consumed.
func ShouldCompact(contextTokens, contextWindow int, settings Settings) bool {
	if !settings.Enabled {
		return false
	}
	if contextWindow <= 0 {
		return false
	}
	settings = settings.WithDefaults()
	return contextTokens > contextWindow-settings.ReserveTokens
}

// EstimateTokens approximates a message's token cost with a chars/4
// heuristic, which overestimates slightly on purpose.
func EstimateTokens(message agent.AgentMessage) int {
	chars := 0

	switch message.Role {
	case "user":
		chars = estimateContentChars(message.UserContent)
	case "assistant":
		for _, block := range message.AssistantContent {
			switch block.Type {
			case "text":
				chars += len(block.Text)
			case "thinking":
				chars += len(block.Thinking)
			case "toolCall":
				chars += len(block.Name) + len(formatToolArgs(block.Arguments))
			}
		}
	case "toolResult":
		for _, block := range message.ToolContent {
			switch block.Type {
			case "text":
				chars += len(block.Text)
			case "image":
				chars += EstimatedImageChars
			}
		}
	default:
		return 0
	}

	return int(math.Ceil(float64(chars) / 4))
}

func estimateContentChars(content any) int {
	switch value := content.(type) {
	case string:
		return len(value)
	case []any:
		chars := 0
		for _, item := range value {
			switch block := item.(type) {
			case ai.TextContent:
				chars += len(block.Text)
			case ai.ImageContent:
				chars += EstimatedImageChars
			case map[string]any:
				switch block["type"] {
				case "text":
					if text, ok := block["text"].(string); ok {
						chars += len(text)
					}
				case "image":
					chars += EstimatedImageChars
				}
			}
		}
		return chars
	case []ai.TextContent:
		chars := 0
		for _, block := range value {
			chars += len(block.Text)
		}
		return chars
	default:
		return 0
	}
}

// isCutPoint reports whether the context can be truncated to start at this
// message. Tool results must stay attached to the assistant message that
// requested them, so they are never cut points.
func isCutPoint(message agent.AgentMessage) bool {
	return message.Role == "user" || message.Role == "assistant"
}

// FindCutPoint returns the index of the first message to keep so that roughly
// keepRecentTokens of recent history survives. It returns 0 when nothing can
// be dropped.
func FindCutPoint(messages []agent.AgentMessage, keepRecentTokens int) int {
	var cutPoints []int
	for i, message := range messages {
		if isCutPoint(message) {
			cutPoints = append(cutPoints, i)
		}
	}
	if len(cutPoints) == 0 {
		return 0
	}

	cutIndex := cutPoints[0]
	accumulated := 0
	for i := len(messages) - 1; i >= 0; i-- {
		tokens := EstimateTokens(messages[i])
		if tokens == 0 {
			continue
		}
		accumulated += tokens
		if accumulated < keepRecentTokens {
			continue
		}
		for _, candidate := range cutPoints {
			if candidate >= i {
				cutIndex = candidate
				break
			}
		}
		break
	}
	return cutIndex
}

// EffectiveKeepRecentTokens shrinks the recent-history budget when the model's
// context window cannot fit both it and the reserved headroom. Without this a
// small-window model would compact into a context that immediately overflows
// again.
func EffectiveKeepRecentTokens(contextWindow int, settings Settings) int {
	settings = settings.WithDefaults()
	keep := settings.KeepRecentTokens
	if contextWindow <= 0 {
		return keep
	}
	budget := contextWindow - 2*settings.ReserveTokens
	if budget < MinKeepRecentTokens {
		budget = MinKeepRecentTokens
	}
	if keep > budget {
		keep = budget
	}
	return keep
}

const summarizationPrompt = `The messages above are a conversation to summarize. Create a structured context checkpoint summary that another LLM will use to continue the work.

Use this EXACT format:

## Goal
[What is the user trying to accomplish? Can be multiple items if the session covers different tasks.]

## Constraints & Preferences
- [Any constraints, preferences, or requirements mentioned by user]
- [Or "(none)" if none were mentioned]

## Progress
### Done
- [x] [Completed tasks/changes]

### In Progress
- [ ] [Current work]

### Blocked
- [Issues preventing progress, if any]

## Key Decisions
- **[Decision]**: [Brief rationale]

## Next Steps
1. [Ordered list of what should happen next]

## Critical Context
- [Any data, examples, or references needed to continue]
- [Or "(none)" if not applicable]

Keep each section concise. Preserve exact file paths, function names, and error messages.`

const updateSummarizationPrompt = `The messages above are NEW conversation messages to incorporate into the existing summary provided in <previous-summary> tags.

Update the existing structured summary with new information. RULES:
- PRESERVE all existing information from the previous summary
- ADD new progress, decisions, and context from the new messages
- UPDATE the Progress section: move items from "In Progress" to "Done" when completed
- UPDATE "Next Steps" based on what was accomplished
- PRESERVE exact file paths, function names, and error messages
- If something is no longer relevant, you may remove it

Use this EXACT format:

## Goal
[Preserve existing goals, add new ones if the task expanded]

## Constraints & Preferences
- [Preserve existing, add new ones discovered]

## Progress
### Done
- [x] [Include previously done items AND newly completed items]

### In Progress
- [ ] [Current work - update based on progress]

### Blocked
- [Current blockers - remove if resolved]

## Key Decisions
- **[Decision]**: [Brief rationale] (preserve all previous, add new)

## Next Steps
1. [Update based on current state]

## Critical Context
- [Preserve important context, add new if needed]

Keep each section concise. Preserve exact file paths, function names, and error messages.`

// SummaryOptions configures a single summarization request.
type SummaryOptions struct {
	// Model is the model used to write the summary.
	Model ai.Model
	// ReserveTokens bounds the summary length (80% of it is the max tokens).
	ReserveTokens int
	// APIKey overrides the provider environment key.
	APIKey string
	// ThinkingLevel is applied only for reasoning models.
	ThinkingLevel agent.ThinkingLevel
	// CustomInstructions adds a focus hint to the summarization prompt.
	CustomInstructions string
	// PreviousSummary switches to the incremental update prompt.
	PreviousSummary string
	// StreamFn overrides ai.StreamSimple, mainly for tests.
	StreamFn ai.StreamFn
}

// GenerateSummary asks the model for a structured summary of messages.
func GenerateSummary(ctx context.Context, messages []agent.AgentMessage, options SummaryOptions) (string, ai.Usage, error) {
	reserveTokens := options.ReserveTokens
	if reserveTokens <= 0 {
		reserveTokens = DefaultReserveTokens
	}
	maxTokens := int(math.Floor(0.8 * float64(reserveTokens)))
	if options.Model.MaxTokens > 0 && options.Model.MaxTokens < maxTokens {
		maxTokens = options.Model.MaxTokens
	}

	basePrompt := summarizationPrompt
	if options.PreviousSummary != "" {
		basePrompt = updateSummarizationPrompt
	}
	if options.CustomInstructions != "" {
		basePrompt += "\n\nAdditional focus: " + options.CustomInstructions
	}

	var prompt strings.Builder
	prompt.WriteString("<conversation>\n")
	prompt.WriteString(SerializeConversation(messages))
	prompt.WriteString("\n</conversation>\n\n")
	if options.PreviousSummary != "" {
		prompt.WriteString("<previous-summary>\n")
		prompt.WriteString(options.PreviousSummary)
		prompt.WriteString("\n</previous-summary>\n\n")
	}
	prompt.WriteString(basePrompt)

	// A summary is a standalone request: isolate its routing and skip cache
	// writes that no later request could reuse.
	streamOptions := &ai.SimpleStreamOptions{}
	streamOptions.MaxTokens = &maxTokens
	streamOptions.APIKey = options.APIKey
	streamOptions.CacheRetention = ai.CacheNone
	streamOptions.SessionID = ai.UUIDv7()
	if signal := signalFromContext(ctx); signal != nil {
		streamOptions.Signal = signal
	}
	if options.Model.Reasoning && options.ThinkingLevel != "" && options.ThinkingLevel != agent.ThinkingOff {
		streamOptions.Reasoning = ai.ThinkingLevel(options.ThinkingLevel)
	}

	request := ai.Context{
		SystemPrompt: SummarizationSystemPrompt,
		Messages: []ai.Message{{
			Role:        "user",
			UserContent: []any{ai.TextContent{Type: "text", Text: prompt.String()}},
			Timestamp:   time.Now().UnixMilli(),
		}},
	}

	streamFn := options.StreamFn
	if streamFn == nil {
		streamFn = ai.StreamSimple
	}
	response := streamFn(options.Model, request, streamOptions).Result()

	if response.StopReason == ai.StopError {
		message := response.ErrorMessage
		if message == "" {
			message = "unknown error"
		}
		return "", response.Usage, fmt.Errorf("summarization failed: %s", message)
	}
	if response.StopReason == ai.StopAborted {
		return "", response.Usage, context.Canceled
	}

	return ai.AssistantText(response), response.Usage, nil
}

// signalFromContext bridges a context to the abort channel the ai package
// expects. It returns nil for contexts that can never be cancelled.
func signalFromContext(ctx context.Context) <-chan struct{} {
	if ctx == nil {
		return nil
	}
	return ctx.Done()
}

// Options configures Compact.
type Options struct {
	// Messages is the current transcript.
	Messages []agent.AgentMessage
	// Model summarizes the dropped history; it is also the source of the
	// context window used to size the recent-history budget.
	Model ai.Model
	// Settings controls the reserve and recent-history budgets.
	Settings Settings
	// APIKey overrides the provider environment key.
	APIKey string
	// ThinkingLevel is applied only for reasoning models.
	ThinkingLevel agent.ThinkingLevel
	// CustomInstructions adds a focus hint to the summarization prompt.
	CustomInstructions string
	// PreviousSummary is the summary produced by an earlier compaction, so
	// repeated compactions accumulate rather than restart.
	PreviousSummary string
	// StreamFn overrides ai.StreamSimple, mainly for tests.
	StreamFn ai.StreamFn
}

// Result describes a completed compaction.
type Result struct {
	// Messages is the replacement transcript: a summary message followed by
	// the retained recent history.
	Messages []agent.AgentMessage
	// Summary is the summary text, including the file operation sections.
	Summary string
	// MessagesRemoved is how many messages were summarized away.
	MessagesRemoved int
	// TokensBefore and TokensAfter are estimates around the compaction.
	TokensBefore int
	TokensAfter  int
	// Usage is what the summarization request cost.
	Usage ai.Usage
}

// SummaryPrefix marks the synthetic user message that carries a summary.
const SummaryPrefix = "<compaction-summary>"

// Compact replaces older history with an LLM-written summary, keeping recent
// messages verbatim. The summary is injected as a user message tagged with
// SummaryPrefix, since this port has no custom transcript message roles.
func Compact(ctx context.Context, options Options) (Result, error) {
	settings := options.Settings.WithDefaults()
	messages := options.Messages

	keepRecent := EffectiveKeepRecentTokens(options.Model.ContextWindow, settings)
	cutIndex := FindCutPoint(messages, keepRecent)
	if cutIndex <= 0 {
		return Result{}, ErrNothingToCompact
	}

	toSummarize := messages[:cutIndex]
	kept := messages[cutIndex:]

	summary, usage, err := GenerateSummary(ctx, toSummarize, SummaryOptions{
		Model:              options.Model,
		ReserveTokens:      settings.ReserveTokens,
		APIKey:             options.APIKey,
		ThinkingLevel:      options.ThinkingLevel,
		CustomInstructions: options.CustomInstructions,
		PreviousSummary:    options.PreviousSummary,
		StreamFn:           options.StreamFn,
	})
	if err != nil {
		return Result{}, err
	}

	ops := NewFileOperations()
	for _, message := range toSummarize {
		ExtractFileOps(message, ops)
	}
	summary += FormatFileOperations(ComputeFileLists(ops))

	compacted := make([]agent.AgentMessage, 0, len(kept)+1)
	compacted = append(compacted, SummaryMessage(summary))
	compacted = append(compacted, kept...)

	return Result{
		Messages:        compacted,
		Summary:         summary,
		MessagesRemoved: cutIndex,
		TokensBefore:    EstimateContextTokens(messages).Tokens,
		TokensAfter:     EstimateContextTokens(compacted).Tokens,
		Usage:           usage,
	}, nil
}

// SummaryMessage wraps a summary in the synthetic user message that replaces
// the dropped history
func SummaryMessage(summary string) agent.AgentMessage {
	text := fmt.Sprintf(
		"%s\nThe conversation so far has been summarized to stay within the context window. Continue from this state.\n\n%s\n</compaction-summary>",
		SummaryPrefix, summary,
	)
	return agent.AgentMessage{
		Role:        "user",
		UserContent: []any{ai.TextContent{Type: "text", Text: text}},
		Timestamp:   time.Now().UnixMilli(),
	}
}

// ExtractPreviousSummary returns the newest summary already present in the
// transcript, so a later compaction can update it instead of starting over.
func ExtractPreviousSummary(messages []agent.AgentMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "user" {
			continue
		}
		text := UserText(messages[i].UserContent)
		if !strings.HasPrefix(text, SummaryPrefix) {
			continue
		}
		text = strings.TrimSuffix(strings.TrimSpace(text), "</compaction-summary>")
		if index := strings.Index(text, "\n\n"); index != -1 {
			return strings.TrimSpace(text[index+2:])
		}
		return strings.TrimSpace(strings.TrimPrefix(text, SummaryPrefix))
	}
	return ""
}

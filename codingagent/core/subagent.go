package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mikus/maiku/agent"
	"github.com/mikus/maiku/ai"
	"github.com/mikus/maiku/codingagent"
)

// SubagentToolName is intentionally not part of the normal built-in tool
// registry. Only a root Maiku session explicitly adds it through
// SelectRootTools, which prevents child agents from recursively delegating.
const SubagentToolName = "subagent"

var subagentSchema = []byte(`{
	"type": "object",
	"properties": {
		"task": {
			"type": "string",
			"minLength": 1,
			"description": "A complete, self-contained task or investigation for the child Maiku agent"
		}
	},
	"required": ["task"],
	"additionalProperties": false
}`)

const subagentReportInstructions = `You are a child Maiku agent working for a root Maiku orchestrator. Complete the delegated task independently using the tools available to you. You cannot create other subagents. Do not ask the user or root agent questions; make reasonable decisions, perform the work, and clearly identify anything that remains unresolved.

Your final response is returned directly to the root agent. It MUST be a concise Markdown report with all of these sections:

## Subagent report
### Investigated or implemented
### Files and actions
### Findings and decisions
### Errors or unresolved issues
### Handoff to root

Put "None" in a section when there is nothing to report. Include paths, commands/tests, errors, and information the root agent needs to continue. Do not include a conversational preamble.`

// SubagentToolOptions configures the child Maiku instances created by a
// SubagentRunner. Runtime model and thinking settings can be changed later
// with SetModel and SetThinkingLevel.
type SubagentToolOptions struct {
	Cwd           string
	AgentDir      string
	Model         ai.Model
	ThinkingLevel agent.ThinkingLevel
	APIKey        string
	Retry         ai.RetryPolicy

	// StreamFn defaults to ai.StreamSimple. It is primarily exposed so hosts
	// with a custom model runtime can give children the same runtime.
	StreamFn agent.StreamFn

	// ChildTools overrides the child's toolset. A nil slice gives every child
	// the normal non-delegating Maiku tools: read, bash, edit, and write.
	// The subagent tool is always removed, even if supplied here.
	ChildTools []agent.AgentTool

	// ChildSystemPrompt replaces the normal coding-agent prompt body when set.
	// The child report contract and non-recursion rule are always appended.
	ChildSystemPrompt string

	// OnEvent observes child lifecycle events. Events from concurrently running
	// children may arrive concurrently, so implementations must be thread-safe.
	OnEvent func(subagentID string, event agent.AgentEvent)
}

// SubagentActivity is a compact, persistable description of one child tool
// action. Inputs and outputs are summarized so reports remain useful after a
// session reload without duplicating large file contents in session history.
type SubagentActivity struct {
	ToolCallID string `json:"toolCallId"`
	ToolName   string `json:"toolName"`
	Input      string `json:"input,omitempty"`
	Output     string `json:"output,omitempty"`
	Status     string `json:"status"`
	IsError    bool   `json:"isError,omitempty"`
}

// SubagentToolDetails is attached to successful tool results and streaming
// updates so hosts can identify, account for, and render a child run.
type SubagentToolDetails struct {
	ID         string             `json:"id"`
	Status     string             `json:"status"`
	DurationMs int64              `json:"durationMs,omitempty"`
	Activities []SubagentActivity `json:"activities,omitempty"`
}

type subagentTrace struct {
	mu         sync.Mutex
	activities []SubagentActivity
}

func (t *subagentTrace) observe(event agent.AgentEvent) {
	t.mu.Lock()
	defer t.mu.Unlock()

	switch event.Type {
	case agent.EventToolExecutionStart:
		t.activities = append(t.activities, SubagentActivity{
			ToolCallID: event.ToolCallID,
			ToolName:   event.ToolName,
			Input:      summarizeSubagentInput(event.ToolName, event.Args),
			Status:     "running",
		})
	case agent.EventToolExecutionEnd:
		for i := len(t.activities) - 1; i >= 0; i-- {
			if t.activities[i].ToolCallID != event.ToolCallID {
				continue
			}
			t.activities[i].Status = "completed"
			t.activities[i].IsError = event.IsError
			if event.IsError {
				t.activities[i].Status = "error"
			}
			t.activities[i].Output = summarizeSubagentResult(event.Result)
			break
		}
	}
}

func (t *subagentTrace) snapshot() []SubagentActivity {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]SubagentActivity(nil), t.activities...)
}

// SubagentRunner owns the lifecycle of child sessions for one root session.
// Each Execute call creates an independent, ephemeral AgentSession. The
// active map is only for cancellation/observability; children share no
// transcript or agent state.
type SubagentRunner struct {
	mu       sync.RWMutex
	options  SubagentToolOptions
	model    ai.Model
	thinking agent.ThinkingLevel
	enabled  bool
	active   map[string]*AgentSession
}

// NewSubagentRunner constructs the root-owned child runner.
func NewSubagentRunner(options SubagentToolOptions) *SubagentRunner {
	return &SubagentRunner{
		options:  options,
		model:    options.Model,
		thinking: options.ThinkingLevel,
		enabled:  true,
		active:   make(map[string]*AgentSession),
	}
}

// SetModel changes the model used by subsequently spawned children. Existing
// children retain the model with which they started.
func (r *SubagentRunner) SetModel(model ai.Model) {
	r.mu.Lock()
	r.model = model
	r.mu.Unlock()
}

// SetThinkingLevel changes the reasoning level used by subsequently spawned
// children. Existing children are unaffected.
func (r *SubagentRunner) SetThinkingLevel(level agent.ThinkingLevel) {
	r.mu.Lock()
	r.thinking = level
	r.mu.Unlock()
}

// SetEnabled controls whether new children may start. Disabling also aborts
// children already running, including calls from a root turn that captured an
// older copy of the tool registry.
func (r *SubagentRunner) SetEnabled(enabled bool) {
	r.mu.Lock()
	r.enabled = enabled
	var children []*AgentSession
	if !enabled {
		children = make([]*AgentSession, 0, len(r.active))
		for _, child := range r.active {
			children = append(children, child)
		}
	}
	r.mu.Unlock()
	for _, child := range children {
		child.Abort()
	}
}

// ActiveCount reports how many child sessions are currently running.
func (r *SubagentRunner) ActiveCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.active)
}

// AbortAll cancels all children currently owned by this runner.
func (r *SubagentRunner) AbortAll() {
	r.mu.RLock()
	children := make([]*AgentSession, 0, len(r.active))
	for _, child := range r.active {
		children = append(children, child)
	}
	r.mu.RUnlock()
	for _, child := range children {
		child.Abort()
	}
}

// Tool returns the first-class tool registered on the root agent. The tool
// keeps the default parallel execution mode, so multiple calls emitted in one
// assistant turn run concurrently in the existing Pi agent loop.
func (r *SubagentRunner) Tool() agent.AgentTool {
	return agent.AgentTool{
		Tool: ai.Tool{
			Name:        SubagentToolName,
			Description: "Delegate one self-contained task to an independent child Maiku agent. The child has read, bash, edit, and write tools, but cannot delegate further. It returns a concise Markdown report. Issue multiple subagent calls in the same response to run independent tasks concurrently.",
			Parameters:  subagentSchema,
		},
		Label: SubagentToolName,
		Execute: func(ctx context.Context, toolCallID string, params map[string]any, onUpdate agent.AgentToolUpdateCallback) (agent.AgentToolResult, error) {
			r.mu.RLock()
			enabled := r.enabled
			r.mu.RUnlock()
			if !enabled {
				return agent.AgentToolResult{}, errors.New(subagentFailureReport("Subagents are disabled by the current settings.", ""))
			}
			task, _ := params["task"].(string)
			task = strings.TrimSpace(task)
			if task == "" {
				return agent.AgentToolResult{}, errors.New(subagentFailureReport("The delegated task was empty.", ""))
			}
			return r.run(ctx, toolCallID, task, onUpdate)
		},
	}
}

func (r *SubagentRunner) runtimeSnapshot() (SubagentToolOptions, ai.Model, agent.ThinkingLevel) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.options, r.model, r.thinking
}

func (r *SubagentRunner) addActive(id string, child *AgentSession) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.enabled {
		return id, false
	}
	// Provider tool-call IDs should be unique. Keep the registry correct even
	// for a malformed/hand-written empty or duplicate id.
	if id == "" || r.active[id] != nil {
		id = ai.UUIDv7()
	}
	r.active[id] = child
	return id, true
}

func (r *SubagentRunner) removeActive(id string, child *AgentSession) {
	r.mu.Lock()
	if r.active[id] == child {
		delete(r.active, id)
	}
	r.mu.Unlock()
}

func (r *SubagentRunner) run(ctx context.Context, id, task string, onUpdate agent.AgentToolUpdateCallback) (agent.AgentToolResult, error) {
	options, model, thinking := r.runtimeSnapshot()
	if id == "" {
		id = ai.UUIDv7()
	}
	started := time.Now()

	childTools := childToolset(options)
	childPrompt := buildSubagentSystemPrompt(options, childTools)
	streamFn := options.StreamFn
	if streamFn == nil {
		streamFn = ai.StreamSimple
	}
	child := NewAgentSession(AgentSessionOptions{
		Model:         model,
		ThinkingLevel: thinking,
		SystemPrompt:  childPrompt,
		Tools:         childTools,
		APIKey:        options.APIKey,
		Retry:         options.Retry,
		StreamFn:      streamFn,
	})
	trace := &subagentTrace{}
	unsubscribe := child.Subscribe(func(event agent.AgentEvent) {
		trace.observe(event)
		if options.OnEvent != nil {
			options.OnEvent(id, event)
		}
	})
	var registered bool
	id, registered = r.addActive(id, child)
	if !registered {
		unsubscribe()
		child.Dispose()
		return agent.AgentToolResult{}, errors.New(subagentFailureReport("Subagents are disabled by the current settings.", ""))
	}
	if onUpdate != nil {
		onUpdate(agent.AgentToolResult{
			Content: []ai.ToolResultContent{{Type: "text", Text: "Subagent started."}},
			Details: SubagentToolDetails{ID: id, Status: "running"},
		})
	}
	defer func() {
		unsubscribe()
		r.removeActive(id, child)
		child.Dispose()
	}()

	if ctx == nil {
		ctx = context.Background()
	}
	if err := child.Prompt(ctx, task); err != nil {
		return agent.AgentToolResult{}, errors.New(subagentFailureReport(err.Error(), child.LastAssistantText()))
	}

	assistant, ok := child.LastAssistantMessage()
	if !ok {
		return agent.AgentToolResult{}, errors.New(subagentFailureReport("The child finished without an assistant response.", ""))
	}
	if assistant.StopReason == ai.StopError || assistant.StopReason == ai.StopAborted {
		reason := assistant.ErrorMessage
		if reason == "" {
			reason = fmt.Sprintf("The child stopped with reason %q.", assistant.StopReason)
		}
		return agent.AgentToolResult{}, errors.New(subagentFailureReport(reason, ai.AssistantText(assistant)))
	}
	if assistant.StopReason == ai.StopLength {
		return agent.AgentToolResult{}, errors.New(subagentFailureReport("The child reached its output token limit, so its report may be incomplete.", ai.AssistantText(assistant)))
	}

	report := ensureSubagentReport(ai.AssistantText(assistant))
	if strings.TrimSpace(report) == "" {
		return agent.AgentToolResult{}, errors.New(subagentFailureReport("The child returned an empty report.", ""))
	}
	usage := aggregateSubagentUsage(child.State().Messages)
	result := agent.AgentToolResult{
		Content: []ai.ToolResultContent{{Type: "text", Text: report}},
		Details: SubagentToolDetails{
			ID:         id,
			Status:     "completed",
			DurationMs: time.Since(started).Milliseconds(),
			Activities: trace.snapshot(),
		},
	}
	if usage.TotalTokens != 0 || usage.Input != 0 || usage.Output != 0 || usage.Cost.Total != 0 {
		result.Usage = &usage
	}
	return result, nil
}

func childToolset(options SubagentToolOptions) []agent.AgentTool {
	var selected []agent.AgentTool
	if options.ChildTools == nil {
		// Keep this explicit rather than calling SelectTools with defaults: the
		// root-only subagent extension can never leak into this child registry.
		selected = SelectTools(options.Cwd, []string{"read", "bash", "edit", "write"}, nil, false)
	} else {
		selected = append([]agent.AgentTool(nil), options.ChildTools...)
	}
	out := selected[:0]
	for _, tool := range selected {
		if tool.Name != SubagentToolName {
			out = append(out, tool)
		}
	}
	return out
}

func buildSubagentSystemPrompt(options SubagentToolOptions, childTools []agent.AgentTool) string {
	names := make([]string, 0, len(childTools))
	for _, tool := range childTools {
		names = append(names, tool.Name)
	}
	agentDir := options.AgentDir
	if agentDir == "" {
		agentDir = codingagent.GetAgentDir()
	}
	contextFiles := LoadProjectContextFiles(options.Cwd, agentDir)
	skills := LoadSkills(LoadSkillsOptions{
		Cwd: options.Cwd, AgentDir: agentDir, IncludeDefaults: true,
	}).Skills
	return BuildSystemPrompt(BuildSystemPromptOptions{
		CustomPrompt:       options.ChildSystemPrompt,
		SelectedTools:      names,
		AppendSystemPrompt: subagentReportInstructions,
		Cwd:                options.Cwd,
		ContextFiles:       contextFiles,
		Skills:             skills,
	})
}

func ensureSubagentReport(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if !strings.Contains(strings.ToLower(text), "## subagent report") {
		text = "## Subagent report\n\n" + text
	}
	required := []string{
		"### Investigated or implemented",
		"### Files and actions",
		"### Findings and decisions",
		"### Errors or unresolved issues",
		"### Handoff to root",
	}
	lower := strings.ToLower(text)
	for _, heading := range required {
		if !strings.Contains(lower, strings.ToLower(heading)) {
			text += "\n\n" + heading + "\n_Not separately reported by the subagent._"
		}
	}
	return text
}

func subagentFailureReport(reason, partial string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "Unknown child-agent failure."
	}
	report := "## Subagent report\n\n" +
		"### Investigated or implemented\nThe delegated task did not complete successfully.\n\n" +
		"### Files and actions\nAny partial workspace changes may remain; inspect the working tree before continuing.\n\n" +
		"### Findings and decisions\nNone reliably reported.\n\n" +
		"### Errors or unresolved issues\n" + reason + "\n\n" +
		"### Handoff to root\nReview the error and current workspace state, then retry or complete the task directly."
	if partial = strings.TrimSpace(partial); partial != "" {
		report += "\n\n### Partial child output\n" + partial
	}
	return report
}

func summarizeSubagentInput(toolName string, args map[string]any) string {
	stringArg := func(key string) string {
		value, _ := args[key].(string)
		return strings.TrimSpace(value)
	}
	var summary string
	switch toolName {
	case "read", "write", "edit":
		summary = stringArg("path")
	case "bash":
		summary = stringArg("command")
	default:
		if path := stringArg("path"); path != "" {
			summary = path
		} else if encoded, err := json.Marshal(args); err == nil {
			summary = string(encoded)
		}
	}
	return truncateSubagentActivity(summary, 280)
}

func summarizeSubagentResult(result *agent.AgentToolResult) string {
	if result == nil {
		return ""
	}
	var text strings.Builder
	for _, content := range result.Content {
		if content.Type != "text" || strings.TrimSpace(content.Text) == "" {
			continue
		}
		if text.Len() > 0 {
			text.WriteByte('\n')
		}
		text.WriteString(content.Text)
	}
	return truncateSubagentActivity(text.String(), 600)
}

func truncateSubagentActivity(value string, limit int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return strings.TrimSpace(string(runes[:limit])) + "…"
}

func aggregateSubagentUsage(messages []agent.AgentMessage) ai.Usage {
	total := ai.EmptyUsage()
	var cacheWrite1h, reasoning int
	var hasCacheWrite1h, hasReasoning bool
	for _, message := range messages {
		if message.Role != "assistant" || message.Usage == nil {
			continue
		}
		u := message.Usage
		total.Input += u.Input
		total.Output += u.Output
		total.CacheRead += u.CacheRead
		total.CacheWrite += u.CacheWrite
		total.TotalTokens += u.TotalTokens
		total.Cost.Input += u.Cost.Input
		total.Cost.Output += u.Cost.Output
		total.Cost.CacheRead += u.Cost.CacheRead
		total.Cost.CacheWrite += u.Cost.CacheWrite
		total.Cost.Total += u.Cost.Total
		if u.CacheWrite1h != nil {
			cacheWrite1h += *u.CacheWrite1h
			hasCacheWrite1h = true
		}
		if u.Reasoning != nil {
			reasoning += *u.Reasoning
			hasReasoning = true
		}
	}
	if hasCacheWrite1h {
		total.CacheWrite1h = &cacheWrite1h
	}
	if hasReasoning {
		total.Reasoning = &reasoning
	}
	return total
}

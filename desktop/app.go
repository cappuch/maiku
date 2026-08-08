package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/mikus/maiku/agent"
	"github.com/mikus/maiku/ai"
	_ "github.com/mikus/maiku/ai/api/anthropic"
	_ "github.com/mikus/maiku/ai/api/google"
	_ "github.com/mikus/maiku/ai/api/openaicompletions"
	_ "github.com/mikus/maiku/ai/api/openairesponses"
	"github.com/mikus/maiku/ai/auth"
	"github.com/mikus/maiku/codingagent"
	"github.com/mikus/maiku/codingagent/core"
	"github.com/mikus/maiku/codingagent/core/compaction"
)

// App is the Wails-bound backend for the maiku desktop UI.
type App struct {
	ctx context.Context

	mu      sync.Mutex
	cwd     string
	model   ai.Model
	thinking agent.ThinkingLevel
	session *core.AgentSession
	mgr     *core.SessionManager
	cancel  context.CancelFunc
	unsub   func()

	usage UsageTotals
}

// UsageTotals is the cumulative token/cost accounting for the open session.
type UsageTotals struct {
	Input      int     `json:"input"`
	Output     int     `json:"output"`
	CacheRead  int     `json:"cacheRead"`
	CacheWrite int     `json:"cacheWrite"`
	TotalTokens int    `json:"totalTokens"`
	Cost       float64 `json:"cost"`
	CacheRate  float64 `json:"cacheRate"`
}

// AppState is returned by GetState.
type AppState struct {
	Cwd           string       `json:"cwd"`
	FolderName    string       `json:"folderName"`
	Provider      string       `json:"provider"`
	ModelID       string       `json:"modelId"`
	ModelName     string       `json:"modelName"`
	Thinking      string       `json:"thinking"`
	Streaming     bool         `json:"streaming"`
	SessionID     string       `json:"sessionId"`
	SessionPath   string       `json:"sessionPath"`
	Usage         UsageTotals  `json:"usage"`
	HasAPIKey     bool         `json:"hasApiKey"`
	Messages      []UIMessage  `json:"messages"`
}

// UIMessage is a frontend-friendly transcript entry.
type UIMessage struct {
	ID        string          `json:"id"`
	Role      string          `json:"role"`
	Text      string          `json:"text,omitempty"`
	ToolName  string          `json:"toolName,omitempty"`
	ToolCallID string         `json:"toolCallId,omitempty"`
	Args      json.RawMessage `json:"args,omitempty"`
	IsError   bool            `json:"isError,omitempty"`
	Streaming bool            `json:"streaming,omitempty"`
}

// ModelInfo is a catalog entry for the model selector.
type ModelInfo struct {
	Provider  string `json:"provider"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	Reasoning bool   `json:"reasoning"`
	HasKey    bool   `json:"hasKey"`
}

// APIKeyStatus shows whether a provider has a stored/env key (never the secret).
type APIKeyStatus struct {
	Provider string `json:"provider"`
	HasKey   bool   `json:"hasKey"`
	Source   string `json:"source"` // "env" | "file" | ""
}

// NewApp creates the application backend.
func NewApp() *App {
	agent.SetDefaultStreamFn(ai.StreamSimple)
	core.InstallAuthStorage(core.DefaultAuthStorage())

	cwd, err := os.Getwd()
	if err != nil {
		cwd = ""
	}
	a := &App{
		cwd:      cwd,
		thinking: core.DefaultThinkingLevel,
	}
	if model, err := core.ResolveModel(core.ResolveModelOptions{RequireAPIKey: false}); err == nil {
		a.model = model
		if !model.Reasoning {
			a.thinking = agent.ThinkingOff
		}
	}
	return a
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	_ = a.ensureSession()
}

func (a *App) emit(name string, data any) {
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, name, data)
}

func (a *App) sessionDir() string {
	a.mu.Lock()
	cwd := a.cwd
	a.mu.Unlock()
	if cwd == "" {
		return codingagent.GetSessionsDir()
	}
	return codingagent.GetDefaultSessionDir(cwd)
}

func (a *App) ensureSession() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.session != nil {
		return nil
	}
	return a.rebuildSessionLocked(nil)
}

func (a *App) rebuildSessionLocked(mgr *core.SessionManager) error {
	if a.unsub != nil {
		a.unsub()
		a.unsub = nil
	}
	if a.session != nil {
		a.session.Dispose()
		a.session = nil
	}

	cwd := a.cwd
	if cwd == "" {
		cwd, _ = os.Getwd()
		a.cwd = cwd
	}
	if mgr == nil {
		dir := codingagent.GetDefaultSessionDir(cwd)
		_ = os.MkdirAll(dir, 0o755)
		mgr = core.NewSessionManager(cwd, dir, true)
	}
	a.mgr = mgr
	a.usage = UsageTotals{}
	a.recomputeUsageLocked(mgr.Messages())

	tools := core.SelectTools(cwd, nil, nil, false)
	toolNames := make([]string, 0, len(tools))
	for _, t := range tools {
		toolNames = append(toolNames, t.Name)
	}
	contextFiles := core.LoadProjectContextFiles(cwd, codingagent.GetAgentDir())
	skills := core.LoadSkills(core.LoadSkillsOptions{
		Cwd: cwd, AgentDir: codingagent.GetAgentDir(), IncludeDefaults: true,
	}).Skills
	systemPrompt := core.BuildSystemPrompt(core.BuildSystemPromptOptions{
		SelectedTools: toolNames,
		Cwd:           cwd,
		ContextFiles:  contextFiles,
		Skills:        skills,
	})

	settings := core.LoadSettings(cwd, codingagent.GetAgentDir())
	session := core.NewAgentSession(core.AgentSessionOptions{
		Model:         a.model,
		ThinkingLevel: a.thinking,
		SystemPrompt:  systemPrompt,
		Tools:         tools,
		Sessions:      mgr,
		Compaction: compaction.Settings{
			Enabled:          settings.Settings.CompactionEnabled(),
			ReserveTokens:    settings.Settings.CompactionReserveTokens(),
			KeepRecentTokens: settings.Settings.CompactionKeepRecentTokens(),
		},
		Retry: ai.RetryPolicy{
			Enabled:     settings.Settings.RetryEnabled(),
			MaxRetries:  settings.Settings.RetryMaxRetries(),
			BaseDelayMs: settings.Settings.RetryBaseDelayMs(),
		},
	})
	a.session = session
	a.unsub = session.Subscribe(a.onAgentEvent)
	return nil
}

func (a *App) onAgentEvent(event agent.AgentEvent) {
	switch event.Type {
	case agent.EventMessageUpdate:
		text := ""
		if event.Message.Role == "assistant" {
			text = assistantText(event.Message)
		}
		a.emit("maiku:message_update", map[string]any{
			"role": event.Message.Role,
			"text": text,
		})
	case agent.EventMessageEnd:
		a.mu.Lock()
		if event.Message.Role == "assistant" && event.Message.Usage != nil {
			a.addUsageLocked(*event.Message.Usage)
		}
		usage := a.usage
		a.mu.Unlock()
		a.emit("maiku:message_end", map[string]any{
			"message": toUIMessage(event.Message),
			"usage":   usage,
		})
	case agent.EventToolExecutionStart:
		args, _ := json.Marshal(event.Args)
		a.emit("maiku:tool_start", map[string]any{
			"toolCallId": event.ToolCallID,
			"toolName":   event.ToolName,
			"args":       json.RawMessage(args),
		})
	case agent.EventToolExecutionUpdate:
		a.emit("maiku:tool_update", map[string]any{
			"toolCallId":   event.ToolCallID,
			"toolName":     event.ToolName,
			"partialResult": event.PartialResult,
		})
	case agent.EventToolExecutionEnd:
		a.emit("maiku:tool_end", map[string]any{
			"toolCallId": event.ToolCallID,
			"toolName":   event.ToolName,
			"result":     event.Result,
			"isError":    event.IsError,
		})
	case agent.EventAgentEnd:
		a.emit("maiku:idle", map[string]any{"ok": true})
	}
}

func assistantText(m ai.Message) string {
	var b strings.Builder
	for _, c := range m.AssistantContent {
		if c.Type == "text" {
			b.WriteString(c.Text)
		}
	}
	return b.String()
}

func toUIMessage(m ai.Message) UIMessage {
	switch m.Role {
	case "user":
		return UIMessage{Role: "user", Text: ai.ContentText(m.UserContent)}
	case "assistant":
		return UIMessage{Role: "assistant", Text: assistantText(m)}
	case "toolResult":
		text := ""
		for _, c := range m.ToolContent {
			if c.Type == "text" {
				text += c.Text
			}
		}
		return UIMessage{
			Role:       "toolResult",
			ToolName:   m.ToolName,
			ToolCallID: m.ToolCallID,
			Text:       text,
			IsError:    m.IsError,
		}
	default:
		return UIMessage{Role: m.Role}
	}
}

func (a *App) addUsageLocked(u ai.Usage) {
	a.usage.Input += u.Input
	a.usage.Output += u.Output
	a.usage.CacheRead += u.CacheRead
	a.usage.CacheWrite += u.CacheWrite
	a.usage.TotalTokens += u.TotalTokens
	a.usage.Cost += u.Cost.Total
	den := a.usage.Input + a.usage.CacheRead
	if den > 0 {
		a.usage.CacheRate = float64(a.usage.CacheRead) / float64(den)
	}
}

func (a *App) recomputeUsageLocked(messages []ai.Message) {
	a.usage = UsageTotals{}
	for _, m := range messages {
		if m.Role == "assistant" && m.Usage != nil && m.StopReason != ai.StopError && m.StopReason != ai.StopAborted {
			a.addUsageLocked(*m.Usage)
		}
	}
}

// GetState returns the current UI state snapshot.
func (a *App) GetState() AppState {
	_ = a.ensureSession()
	a.mu.Lock()
	defer a.mu.Unlock()

	folder := ""
	if a.cwd != "" {
		folder = filepath.Base(a.cwd)
	}
	sessionID, sessionPath := "", ""
	var messages []UIMessage
	streaming := false
	if a.mgr != nil {
		sessionID = a.mgr.Header().ID
		sessionPath = a.mgr.File()
		for _, m := range a.mgr.Messages() {
			messages = append(messages, toUIMessage(m))
		}
	}
	if a.session != nil {
		streaming = a.session.State().IsStreaming
	}
	return AppState{
		Cwd:         a.cwd,
		FolderName:  folder,
		Provider:    a.model.Provider,
		ModelID:     a.model.ID,
		ModelName:   a.model.Name,
		Thinking:    string(a.thinking),
		Streaming:   streaming,
		SessionID:   sessionID,
		SessionPath: sessionPath,
		Usage:       a.usage,
		HasAPIKey:   auth.ResolveAPIKey(a.model.Provider) != "",
		Messages:    messages,
	}
}

// ListModels returns the built-in model catalog.
func (a *App) ListModels() []ModelInfo {
	var out []ModelInfo
	for _, p := range core.AllProviders() {
		has := auth.ResolveAPIKey(p.ID) != ""
		for _, m := range p.Models {
			out = append(out, ModelInfo{
				Provider:  p.ID,
				ID:        m.ID,
				Name:      m.Name,
				Reasoning: m.Reasoning,
				HasKey:    has,
			})
		}
	}
	return out
}

// SetModel switches the active model.
func (a *App) SetModel(provider, modelID string) error {
	model, err := core.ResolveModel(core.ResolveModelOptions{Provider: provider, Model: modelID})
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.model = model
	if !model.Reasoning {
		a.thinking = agent.ThinkingOff
	}
	return a.rebuildSessionLocked(a.mgr)
}

// SetThinking sets the thinking level.
func (a *App) SetThinking(level string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.thinking = agent.ThinkingLevel(level)
	return a.rebuildSessionLocked(a.mgr)
}

// ListSessions returns session summaries for the sessions root (all workspaces).
func (a *App) ListSessions() []core.SessionSummary {
	root := codingagent.GetSessionsDir()
	return core.ListSessionSummaries(root)
}

// NewSession starts a fresh session in the current folder.
func (a *App) NewSession() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	dir := codingagent.GetDefaultSessionDir(a.cwd)
	_ = os.MkdirAll(dir, 0o755)
	mgr := core.NewSessionManager(a.cwd, dir, true)
	return a.rebuildSessionLocked(mgr)
}

// OpenSession loads an existing session file.
func (a *App) OpenSession(path string) error {
	mgr, err := core.LoadSessionManager(path)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if mgr.Header().Cwd != "" {
		a.cwd = mgr.Header().Cwd
	}
	return a.rebuildSessionLocked(mgr)
}

// OpenFolder opens a native directory picker and switches workspace.
func (a *App) OpenFolder() (string, error) {
	path, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Open project folder",
	})
	if err != nil {
		return "", err
	}
	if path == "" {
		return a.cwd, nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cwd = path
	dir := codingagent.GetDefaultSessionDir(path)
	_ = os.MkdirAll(dir, 0o755)
	mgr := core.NewSessionManager(path, dir, true)
	if err := a.rebuildSessionLocked(mgr); err != nil {
		return "", err
	}
	return path, nil
}

// ListAPIKeys returns which providers have credentials configured.
func (a *App) ListAPIKeys() []APIKeyStatus {
	store := core.DefaultAuthStorage()
	var out []APIKeyStatus
	seen := map[string]bool{}
	for _, p := range core.AllProviders() {
		if seen[p.ID] {
			continue
		}
		seen[p.ID] = true
		status := APIKeyStatus{Provider: p.ID}
		if auth.FindEnvVar(p.ID) != "" {
			status.HasKey = true
			status.Source = "env"
		} else if store.APIKey(p.ID) != "" {
			status.HasKey = true
			status.Source = "file"
		}
		out = append(out, status)
	}
	return out
}

// SetAPIKey stores an API key for a provider in auth.json.
func (a *App) SetAPIKey(provider, key string) error {
	provider = strings.TrimSpace(provider)
	key = strings.TrimSpace(key)
	if provider == "" {
		return fmt.Errorf("provider is required")
	}
	store := core.DefaultAuthStorage()
	if key == "" {
		return store.Delete(provider)
	}
	return store.Write(provider, core.Credential{Type: core.CredentialAPIKey, Key: key})
}

// Prompt sends a user message and streams events until idle.
func (a *App) Prompt(text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("empty prompt")
	}
	if err := a.ensureSession(); err != nil {
		return err
	}

	a.mu.Lock()
	if a.cancel != nil {
		a.mu.Unlock()
		return fmt.Errorf("already streaming")
	}
	if auth.ResolveAPIKey(a.model.Provider) == "" {
		provider := a.model.Provider
		a.mu.Unlock()
		return fmt.Errorf("no API key for provider %q — add one in Settings", provider)
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.cancel = cancel
	session := a.session
	a.mu.Unlock()

	a.emit("maiku:message_end", map[string]any{
		"message": UIMessage{Role: "user", Text: text},
	})

	go func() {
		defer func() {
			a.mu.Lock()
			a.cancel = nil
			a.mu.Unlock()
			a.emit("maiku:idle", map[string]any{"ok": true})
		}()
		if err := session.Prompt(ctx, text); err != nil {
			a.emit("maiku:error", map[string]any{"error": err.Error()})
		}
	}()
	return nil
}

// Abort cancels the in-flight prompt.
func (a *App) Abort() {
	a.mu.Lock()
	cancel := a.cancel
	session := a.session
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if session != nil {
		session.Abort()
	}
}

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/mikus/maiku/agent"
	"github.com/mikus/maiku/ai"
	_ "github.com/mikus/maiku/ai/api/anthropic"
	_ "github.com/mikus/maiku/ai/api/google"
	_ "github.com/mikus/maiku/ai/api/openaicompletions"
	_ "github.com/mikus/maiku/ai/api/openaicodex"
	_ "github.com/mikus/maiku/ai/api/openairesponses"
	"github.com/mikus/maiku/ai/auth"
	openaicodexoauth "github.com/mikus/maiku/ai/auth/openaicodex"
	"github.com/mikus/maiku/codingagent"
	"github.com/mikus/maiku/codingagent/cli"
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
	// promptGen identifies the active Prompt goroutine so a cancelled/switched
	// run cannot clear a newer cancel or emit idle/error into the new session.
	promptGen uint64
	unsub     func()

	usage UsageTotals

	recentDirs []string

	codexLoginMu     sync.Mutex
	codexLoginCancel context.CancelFunc
	codexPollCtx     context.Context
	codexDevice      *openaicodexoauth.DeviceAuth
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
	RecentDirs    []string     `json:"recentDirs"`
}

// UIMessage is a frontend-friendly transcript entry.
type UIMessage struct {
	ID         string            `json:"id"`
	Role       string            `json:"role"`
	Text       string            `json:"text,omitempty"`
	ToolName   string            `json:"toolName,omitempty"`
	ToolCallID string            `json:"toolCallId,omitempty"`
	Args       json.RawMessage   `json:"args,omitempty"`
	Details    any               `json:"details,omitempty"`
	IsError    bool              `json:"isError,omitempty"`
	Streaming  bool              `json:"streaming,omitempty"`
	Images     []ImageAttachment `json:"images,omitempty"`
}

// ModelInfo is a catalog entry for the model selector.
type ModelInfo struct {
	Provider  string `json:"provider"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	Reasoning bool   `json:"reasoning"`
	Vision    bool   `json:"vision"`
	HasKey    bool   `json:"hasKey"`
}

// APIKeyStatus shows whether a provider has a stored/env key (never the secret).
type APIKeyStatus struct {
	Provider string `json:"provider"`
	Name     string `json:"name"`
	HasKey   bool   `json:"hasKey"`
	Source   string `json:"source"` // "env" | "file" | ""
}

// NewApp creates the application backend.
func NewApp() *App {
	agent.SetDefaultStreamFn(ai.StreamSimple)
	core.InstallAuthStorage(core.DefaultAuthStorage())

	// Reopen the most recently used workspace (if any) instead of the process
	// CWD, so the header indicator reflects where you last worked and the
	// launch directory doesn't pollute the recent-folder list.
	recentDirs := loadRecentDirs()
	cwd := ""
	if len(recentDirs) > 0 {
		cwd = recentDirs[0]
	} else if wd, err := os.Getwd(); err == nil {
		cwd = wd
	}
	a := &App{
		cwd:        cwd,
		thinking:   core.DefaultThinkingLevel,
		recentDirs: recentDirs,
	}
	if cwd != "" {
		a.rememberDir(cwd)
	}

	settings := core.LoadSettings(cwd, codingagent.GetAgentDir())
	providerID := settings.Settings.DefaultProvider
	if providerID == "" {
		for _, p := range core.AllProviders() {
			if auth.ResolveAPIKey(p.ID) != "" {
				providerID = p.ID
				break
			}
		}
	}
	if providerID != "" {
		_ = core.RefreshProviderModels(context.Background(), providerID)
	}

	opts := core.ResolveModelOptions{
		Provider:      providerID,
		Model:         settings.Settings.DefaultModel,
		RequireAPIKey: false,
	}
	if model, err := core.ResolveModel(opts); err == nil {
		a.model = model
		a.applyModelThinkingLocked(settings.Settings)
	}
	return a
}

// applyModelThinkingLocked sets the reasoning level for the active model: a
// saved per-model level wins, otherwise reasoning-capable models default to
// medium and non-reasoning models run with thinking off.
func (a *App) applyModelThinkingLocked(settings core.Settings) {
	if !a.model.Reasoning {
		a.thinking = agent.ThinkingOff
		return
	}
	if level, ok := settings.ThinkingLevelForModel(a.model.ID); ok && cli.IsValidThinkingLevel(level) {
		a.thinking = agent.ThinkingLevel(level)
		return
	}
	a.thinking = core.DefaultThinkingLevel
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.refreshConfiguredModels()
	// Re-resolve after fetch in case startup had an empty catalog.
	a.mu.Lock()
	settings := core.LoadSettings(a.cwd, codingagent.GetAgentDir())
	providerID := a.model.Provider
	if providerID == "" {
		providerID = settings.Settings.DefaultProvider
	}
	modelID := a.model.ID
	if modelID == "" {
		modelID = settings.Settings.DefaultModel
	}
	a.mu.Unlock()
	if providerID != "" {
		if model, err := core.ResolveModel(core.ResolveModelOptions{
			Provider: providerID,
			Model:    modelID,
		}); err == nil {
			a.mu.Lock()
			a.model = model
			a.applyModelThinkingLocked(settings.Settings)
			a.mu.Unlock()
		}
	}
	_ = a.ensureSession()
}

// refreshConfiguredModels fetches /models for every provider that has a key,
// plus the active provider, so the picker is populated from live catalogs only.
func (a *App) refreshConfiguredModels() {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	a.mu.Lock()
	active := a.model.Provider
	a.mu.Unlock()

	seen := map[string]bool{}
	var ids []string
	if active != "" {
		ids = append(ids, active)
		seen[active] = true
	}
	for _, p := range core.AllProviders() {
		if seen[p.ID] {
			continue
		}
		if p.ID == "openai-codex" || auth.ResolveAPIKey(p.ID) != "" {
			ids = append(ids, p.ID)
			seen[p.ID] = true
		}
	}
	for _, id := range ids {
		_ = core.RefreshProviderModels(ctx, id)
	}
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
	// Cancel any in-flight prompt before tearing down the session so late
	// stream/idle events cannot land on the replacement session.
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
	a.promptGen++
	if a.session != nil {
		a.session.Abort()
	}
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
	_ = mgr.EnsurePersisted()
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
		var toolCalls []map[string]any
		chars := 0
		if event.Message.Role == "assistant" {
			text = assistantText(event.Message)
			toolCalls = partialToolCalls(event.Message)
			chars = streamedChars(event.Message)
		}
		payload := map[string]any{
			"role":  event.Message.Role,
			"text":  text,
			"chars": chars,
		}
		if len(toolCalls) > 0 {
			payload["toolCalls"] = toolCalls
		}
		a.emit("maiku:message_update", payload)
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
			"toolCallId":    event.ToolCallID,
			"toolName":      event.ToolName,
			"partialResult": event.PartialResult,
		})
	case agent.EventToolExecutionEnd:
		var details any
		var resultText string
		if event.Result != nil {
			details = event.Result.Details
			for _, c := range event.Result.Content {
				if c.Type == "text" && c.Text != "" {
					if resultText != "" {
						resultText += "\n"
					}
					resultText += c.Text
				}
			}
		}
		a.emit("maiku:tool_end", map[string]any{
			"toolCallId": event.ToolCallID,
			"toolName":   event.ToolName,
			"result":     event.Result,
			"details":    details,
			"resultText": resultText,
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

// streamedChars counts the characters streamed so far (text + thinking +
// tool-call JSON). The live tok/s estimate uses this instead of visible text
// alone so the rate moves during the reasoning phase rather than sitting at 0
// until the final answer text streams.
func streamedChars(m ai.Message) int {
	n := 0
	for _, c := range m.AssistantContent {
		switch c.Type {
		case "text":
			n += len(c.Text)
		case "thinking":
			n += len(c.Thinking)
		case "toolCall":
			if b, err := json.Marshal(c.Arguments); err == nil {
				n += len(b)
			}
		}
	}
	return n
}

// partialToolCalls extracts in-progress tool calls from a streaming assistant
// message so the UI can preview write/edit args as they arrive.
func partialToolCalls(m ai.Message) []map[string]any {
	var out []map[string]any
	for _, c := range m.AssistantContent {
		if c.Type != "toolCall" {
			continue
		}
		out = append(out, map[string]any{
			"toolCallId": c.ID,
			"toolName":   c.Name,
			"args":       c.Arguments,
		})
	}
	return out
}

func toUIMessage(m ai.Message) UIMessage {
	switch m.Role {
	case "user":
		raw := ai.ContentText(m.UserContent)
		return UIMessage{
			Role:   "user",
			Text:   stripInjectedFiles(raw),
			Images: extractUserImages(m.UserContent),
		}
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
			Details:    m.Details,
			IsError:    m.IsError,
		}
	default:
		return UIMessage{Role: m.Role}
	}
}

// transcriptUIMessages expands assistant tool calls into tool cards and merges
// matching tool results so write/edit/read keep their path + preview after reload.
func transcriptUIMessages(messages []ai.Message) []UIMessage {
	resultsByID := map[string]ai.Message{}
	for _, m := range messages {
		if m.Role == "toolResult" && m.ToolCallID != "" {
			resultsByID[m.ToolCallID] = m
		}
	}

	var out []UIMessage
	seenResults := map[string]bool{}
	for _, m := range messages {
		switch m.Role {
		case "user":
			out = append(out, toUIMessage(m))
		case "assistant":
			if text := assistantText(m); text != "" {
				out = append(out, UIMessage{Role: "assistant", Text: text})
			}
			for _, c := range m.AssistantContent {
				if c.Type != "toolCall" {
					continue
				}
				ui := UIMessage{
					Role:       "tool",
					ToolCallID: c.ID,
					ToolName:   c.Name,
					Args:       mustJSON(c.Arguments),
				}
				if result, ok := resultsByID[c.ID]; ok {
					seenResults[c.ID] = true
					text := ""
					for _, tc := range result.ToolContent {
						if tc.Type == "text" {
							text += tc.Text
						}
					}
					ui.Text = text
					ui.Details = result.Details
					ui.IsError = result.IsError
				}
				out = append(out, ui)
			}
		case "toolResult":
			if seenResults[m.ToolCallID] {
				continue
			}
			out = append(out, toUIMessage(m))
		default:
			out = append(out, toUIMessage(m))
		}
	}
	return out
}

func mustJSON(v any) json.RawMessage {
	if v == nil {
		return json.RawMessage("{}")
	}
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("{}")
	}
	return b
}

// stripInjectedFiles removes <file path="...">...</file> blocks that were
// prepended for @-mention context injection, leaving the user's visible text.
var injectedFileRe = regexp.MustCompile(`(?s)<file path="[^"]*">.*?</file>\s*`)

func stripInjectedFiles(text string) string {
	cleaned := strings.TrimSpace(injectedFileRe.ReplaceAllString(text, ""))
	if cleaned == "" && strings.Contains(text, "<file path=") {
		// Message was only attachments — show a short stand-in.
		return "(attached files)"
	}
	return cleaned
}

func extractUserImages(content any) []ImageAttachment {
	parts, ok := content.([]any)
	if !ok {
		return nil
	}
	var out []ImageAttachment
	for _, item := range parts {
		switch v := item.(type) {
		case ai.ImageContent:
			out = append(out, ImageAttachment{MimeType: v.MimeType, Data: v.Data})
		case map[string]any:
			if v["type"] != "image" {
				continue
			}
			mime, _ := v["mimeType"].(string)
			data, _ := v["data"].(string)
			if mime != "" && data != "" {
				out = append(out, ImageAttachment{MimeType: mime, Data: data})
			}
		}
	}
	return out
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
		messages = transcriptUIMessages(a.mgr.Messages())
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
		RecentDirs:  append([]string(nil), a.recentDirs...),
	}
}

// ListModels returns the model catalog, enriched with the configured
// provider's remote /models route (vision, context, newly listed ids).
func (a *App) ListModels() []ModelInfo {
	a.refreshConfiguredModels()
	var out []ModelInfo
	for _, p := range core.AllProviders() {
		has := auth.ResolveAPIKey(p.ID) != ""
		for _, m := range p.Models {
			vision := false
			for _, in := range m.Input {
				if in == "image" {
					vision = true
					break
				}
			}
			out = append(out, ModelInfo{
				Provider:  p.ID,
				ID:        m.ID,
				Name:      m.Name,
				Reasoning: m.Reasoning,
				Vision:    vision,
				HasKey:    has,
			})
		}
	}
	return out
}

// SetModel switches the active model and persists it to settings.json.
func (a *App) SetModel(provider, modelID string) error {
	// Refresh this provider so ResolveModel can see newly listed remote ids.
	_ = core.RefreshProviderModels(context.Background(), provider)
	model, err := core.ResolveModel(core.ResolveModelOptions{Provider: provider, Model: modelID})
	if err != nil {
		return err
	}
	_ = core.SetDefaultModel(codingagent.GetAgentDir(), provider, modelID)

	a.mu.Lock()
	defer a.mu.Unlock()
	a.model = model
	a.applyModelThinkingLocked(core.LoadSettings(a.cwd, codingagent.GetAgentDir()).Settings)
	return a.rebuildSessionLocked(a.mgr)
}

// SetThinking sets the thinking level and saves it for the active model id, so
// each model keeps its own reasoning preference.
func (a *App) SetThinking(level string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.thinking = agent.ThinkingLevel(level)
	if a.model.ID != "" {
		if err := core.SetModelThinkingLevel(codingagent.GetAgentDir(), a.model.ID, level); err != nil {
			return err
		}
	}
	return a.rebuildSessionLocked(a.mgr)
}

// RecentDirs returns the five most recently opened directories, newest first.
func (a *App) RecentDirs() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.recentDirs...)
}

// OpenRecentFolder switches to a directory selected from the recent-directory list.
func (a *App) OpenRecentFolder(path string) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("not a directory: %s", path)
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	a.cwd = path
	a.rememberDirLocked(path)
	dir := codingagent.GetDefaultSessionDir(path)
	_ = os.MkdirAll(dir, 0o755)
	return a.rebuildSessionLocked(core.NewSessionManager(path, dir, true))
}

// ListSessions returns session summaries for the sessions root (all workspaces).
func (a *App) ListSessions() []core.SessionSummary {
	root := codingagent.GetSessionsDir()
	return core.ListSessionSummaries(root)
}

// RenameSession sets a display name for a session file. An empty name clears it.
func (a *App) RenameSession(path, name string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("session path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if !strings.HasSuffix(abs, ".jsonl") {
		return fmt.Errorf("not a session file: %s", path)
	}
	if _, err := os.Stat(abs); err != nil {
		return err
	}
	return core.SaveSessionName(abs, name)
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
		a.rememberDirLocked(a.cwd)
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
	a.rememberDirLocked(path)
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
		status := APIKeyStatus{Provider: p.ID, Name: p.Name}
		if auth.FindEnvVar(p.ID) != "" {
			status.HasKey = true
			status.Source = "env"
		} else if cred, ok := store.Read(p.ID); ok {
			switch cred.Type {
			case core.CredentialOAuth:
				if cred.Access != "" {
					status.HasKey = true
					status.Source = "oauth"
				}
			case core.CredentialAPIKey:
				if store.APIKey(p.ID) != "" {
					status.HasKey = true
					status.Source = "file"
				}
			}
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

// CodexLoginInfo is returned when starting ChatGPT Codex device login.
type CodexLoginInfo struct {
	UserCode        string `json:"userCode"`
	VerificationURI string `json:"verificationUri"`
}

// BeginOpenAICodexLogin starts the device-code OAuth flow for openai-codex.
func (a *App) BeginOpenAICodexLogin() (*CodexLoginInfo, error) {
	a.CancelOpenAICodexLogin()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	device, err := openaicodexoauth.StartDeviceAuth(ctx)
	if err != nil {
		return nil, err
	}

	pollCtx, pollCancel := context.WithCancel(context.Background())
	a.codexLoginMu.Lock()
	a.codexDevice = device
	a.codexPollCtx = pollCtx
	a.codexLoginCancel = pollCancel
	a.codexLoginMu.Unlock()

	return &CodexLoginInfo{
		UserCode:        device.UserCode,
		VerificationURI: device.VerificationURI,
	}, nil
}

// FinishOpenAICodexLogin polls until device login completes and stores the OAuth credential.
func (a *App) FinishOpenAICodexLogin() error {
	a.codexLoginMu.Lock()
	device := a.codexDevice
	pollCtx := a.codexPollCtx
	a.codexLoginMu.Unlock()
	if device == nil || pollCtx == nil {
		return fmt.Errorf("call BeginOpenAICodexLogin first")
	}

	token, err := openaicodexoauth.PollDeviceAuth(pollCtx, device)
	if err != nil {
		return err
	}
	cred := core.Credential{
		Type:    core.CredentialOAuth,
		Access:  token.Access,
		Refresh: token.Refresh,
		Expires: token.Expires.UnixMilli(),
	}
	if err := core.DefaultAuthStorage().Write("openai-codex", cred); err != nil {
		return err
	}
	_ = core.RefreshProviderModels(context.Background(), "openai-codex")
	a.CancelOpenAICodexLogin()
	return nil
}

// CancelOpenAICodexLogin aborts an in-progress Codex device login.
func (a *App) CancelOpenAICodexLogin() {
	a.codexLoginMu.Lock()
	defer a.codexLoginMu.Unlock()
	if a.codexLoginCancel != nil {
		a.codexLoginCancel()
		a.codexLoginCancel = nil
	}
	a.codexPollCtx = nil
	a.codexDevice = nil
}

// Prompt sends a user message (with optional image attachments) and streams
// events until idle. @path mentions in text are expanded into file contents.
func (a *App) Prompt(text string, images []ImageAttachment) error {
	displayText := strings.TrimSpace(text)
	if displayText == "" && len(images) == 0 {
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
	cwd := a.cwd
	ctx, cancel := context.WithCancel(a.ctx)
	a.cancel = cancel
	a.promptGen++
	promptGen := a.promptGen
	session := a.session
	a.mu.Unlock()

	expanded, err := core.ExpandAtMentions(cwd, displayText)
	if err != nil {
		a.mu.Lock()
		if a.promptGen == promptGen {
			a.cancel = nil
		}
		a.mu.Unlock()
		cancel()
		return err
	}
	promptText := expanded.Text
	if strings.TrimSpace(promptText) == "" {
		promptText = "(image)"
	}

	var aiImages []ai.ImageContent
	for _, img := range images {
		if img.Data == "" || img.MimeType == "" {
			continue
		}
		aiImages = append(aiImages, ai.ImageContent{
			Type:     "image",
			Data:     img.Data,
			MimeType: img.MimeType,
		})
	}
	aiImages = append(aiImages, expanded.Images...)

	uiImages := images
	if len(uiImages) == 0 && len(expanded.Images) > 0 {
		uiImages = make([]ImageAttachment, 0, len(expanded.Images))
		for _, img := range expanded.Images {
			uiImages = append(uiImages, ImageAttachment{MimeType: img.MimeType, Data: img.Data})
		}
	}

	display := displayText
	if display == "" {
		display = "(image)"
	}
	a.emit("maiku:message_end", map[string]any{
		"message": UIMessage{Role: "user", Text: display, Images: uiImages},
	})

	go func() {
		defer func() {
			a.mu.Lock()
			active := a.promptGen == promptGen
			if active {
				a.cancel = nil
			}
			a.mu.Unlock()
			if active {
				a.emit("maiku:idle", map[string]any{"ok": true})
			}
		}()
		if err := session.Prompt(ctx, promptText, aiImages...); err != nil {
			a.mu.Lock()
			active := a.promptGen == promptGen
			a.mu.Unlock()
			if active {
				a.emit("maiku:error", map[string]any{"error": err.Error()})
			}
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

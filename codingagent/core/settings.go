package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mikus/maiku/codingagent"
)

// Defaults mirroring the TypeScript settings manager and compaction module.
const (
	DefaultCompactionReserveTokens    = 16384
	DefaultCompactionKeepRecentTokens = 20000
	DefaultBranchSummaryReserveTokens = 16384
	DefaultRetryMaxRetries            = 3
	DefaultRetryBaseDelayMs           = 2000
	DefaultMaxRetryDelayMs            = 60000
	DefaultTransport                  = "auto"
)

// CompactionSettings controls automatic context compaction.
type CompactionSettings struct {
	Enabled          *bool `json:"enabled,omitempty"`
	ReserveTokens    *int  `json:"reserveTokens,omitempty"`
	KeepRecentTokens *int  `json:"keepRecentTokens,omitempty"`
}

// BranchSummarySettings controls branch summarization.
type BranchSummarySettings struct {
	ReserveTokens *int  `json:"reserveTokens,omitempty"`
	SkipPrompt    *bool `json:"skipPrompt,omitempty"`
}

// ProviderRetrySettings are the provider/SDK level retry knobs.
type ProviderRetrySettings struct {
	TimeoutMs       *int `json:"timeoutMs,omitempty"`
	MaxRetries      *int `json:"maxRetries,omitempty"`
	MaxRetryDelayMs *int `json:"maxRetryDelayMs,omitempty"`
}

// RetrySettings controls agent-level retries of failed assistant turns.
type RetrySettings struct {
	Enabled     *bool                  `json:"enabled,omitempty"`
	MaxRetries  *int                   `json:"maxRetries,omitempty"`
	BaseDelayMs *int                   `json:"baseDelayMs,omitempty"`
	Provider    *ProviderRetrySettings `json:"provider,omitempty"`
}

// Settings is the subset of the TypeScript Settings schema this port reads.
// Unknown keys are preserved in SettingsResult.Raw rather than dropped, so a
// settings.json written by the TypeScript CLI round-trips without loss when
// only inspected here.
type Settings struct {
	DefaultProvider      string            `json:"defaultProvider,omitempty"`
	DefaultModel         string            `json:"defaultModel,omitempty"`
	DefaultThinkingLevel string            `json:"defaultThinkingLevel,omitempty"`
	// Reasoning maps a model id to its saved reasoning level (off, low, medium,
	// high, …). Each model keeps its own preference; models without an entry
	// fall back to the default level.
	Reasoning map[string]string `json:"reasoning,omitempty"`
	Transport string            `json:"transport,omitempty"`
	Theme                string                 `json:"theme,omitempty"`
	ShellPath            string                 `json:"shellPath,omitempty"`
	ShellCommandPrefix   string                 `json:"shellCommandPrefix,omitempty"`
	SessionDir           string                 `json:"sessionDir,omitempty"`
	Compaction           *CompactionSettings    `json:"compaction,omitempty"`
	BranchSummary        *BranchSummarySettings `json:"branchSummary,omitempty"`
	Retry                *RetrySettings         `json:"retry,omitempty"`
	Skills               []string               `json:"skills,omitempty"`
	EnableSkillCommands  *bool                  `json:"enableSkillCommands,omitempty"`
	EnabledModels        []string               `json:"enabledModels,omitempty"`
	QuietStartup         *bool                  `json:"quietStartup,omitempty"`
	HTTPProxy            string                 `json:"httpProxy,omitempty"`
	HTTPIdleTimeoutMs    *int                   `json:"httpIdleTimeoutMs,omitempty"`
}

// SettingsScope identifies which settings file a value or error came from.
type SettingsScope string

const (
	ScopeGlobal  SettingsScope = "global"
	ScopeProject SettingsScope = "project"
)

// SettingsError reports a settings file that could not be read or parsed.
// A broken file is skipped rather than fatal, matching the TypeScript loader.
type SettingsError struct {
	Scope SettingsScope
	Path  string
	Err   error
}

func (e SettingsError) Error() string {
	return fmt.Sprintf("%s settings (%s): %v", e.Scope, e.Path, e.Err)
}

func (e SettingsError) Unwrap() error { return e.Err }

// SettingsResult is the merged view of the global and project settings files.
type SettingsResult struct {
	// Settings is the merged, typed view: project overrides global.
	Settings Settings
	// Global and Project are the per-scope typed views.
	Global  Settings
	Project Settings
	// Raw is the merged untyped view, including keys this port does not model.
	Raw map[string]any
	// GlobalPath and ProjectPath are the files that were consulted.
	GlobalPath  string
	ProjectPath string
	// Errors holds per-scope load failures.
	Errors []SettingsError
}

// GlobalSettingsPath returns the path of the user-level settings.json.
func GlobalSettingsPath(agentDir string) string {
	return filepath.Join(agentDir, "settings.json")
}

// ProjectSettingsPath returns the path of the project-level settings.json.
func ProjectSettingsPath(cwd string) string {
	return filepath.Join(cwd, codingagent.CONFIG_DIR_NAME, "settings.json")
}

// LoadSettings reads and merges ~/.maiku/agent/settings.json and
// <cwd>/.maiku/settings.json. Missing files are not errors.
func LoadSettings(cwd, agentDir string) SettingsResult {
	if agentDir == "" {
		agentDir = codingagent.GetAgentDir()
	}
	result := SettingsResult{
		GlobalPath:  GlobalSettingsPath(codingagent.ExpandTildePath(agentDir)),
		ProjectPath: ProjectSettingsPath(codingagent.ExpandTildePath(cwd)),
		Raw:         map[string]any{},
	}

	globalRaw, err := readSettingsFile(result.GlobalPath)
	if err != nil {
		result.Errors = append(result.Errors, SettingsError{Scope: ScopeGlobal, Path: result.GlobalPath, Err: err})
	}
	projectRaw, err := readSettingsFile(result.ProjectPath)
	if err != nil {
		result.Errors = append(result.Errors, SettingsError{Scope: ScopeProject, Path: result.ProjectPath, Err: err})
	}

	result.Raw = deepMergeSettingsMaps(globalRaw, projectRaw)
	result.Global = decodeSettings(globalRaw)
	result.Project = decodeSettings(projectRaw)
	result.Settings = decodeSettings(result.Raw)
	return result
}

func readSettingsFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func decodeSettings(raw map[string]any) Settings {
	var settings Settings
	if len(raw) == 0 {
		return settings
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return settings
	}
	// Type mismatches in a hand-edited file should not lose the fields that
	// did decode, so the error is deliberately ignored here.
	_ = json.Unmarshal(data, &settings)
	return settings
}

// deepMergeSettingsMaps merges overrides into base, recursing into nested
// objects. Arrays and scalars are replaced wholesale, matching
// deepMergeSettings in the TypeScript settings manager.
func deepMergeSettingsMaps(base, overrides map[string]any) map[string]any {
	merged := make(map[string]any, len(base)+len(overrides))
	for key, value := range base {
		merged[key] = value
	}
	for key, overrideValue := range overrides {
		if overrideValue == nil {
			continue
		}
		baseChild, baseIsObject := merged[key].(map[string]any)
		overrideChild, overrideIsObject := overrideValue.(map[string]any)
		if baseIsObject && overrideIsObject {
			merged[key] = deepMergeSettingsMaps(baseChild, overrideChild)
			continue
		}
		merged[key] = overrideValue
	}
	return merged
}

// CompactionEnabled reports whether automatic compaction is on (default true).
func (s Settings) CompactionEnabled() bool {
	if s.Compaction != nil && s.Compaction.Enabled != nil {
		return *s.Compaction.Enabled
	}
	return true
}

// CompactionReserveTokens is the headroom kept below the context window.
func (s Settings) CompactionReserveTokens() int {
	if s.Compaction != nil && s.Compaction.ReserveTokens != nil && *s.Compaction.ReserveTokens > 0 {
		return *s.Compaction.ReserveTokens
	}
	return DefaultCompactionReserveTokens
}

// CompactionKeepRecentTokens is the recent-history budget kept verbatim.
func (s Settings) CompactionKeepRecentTokens() int {
	if s.Compaction != nil && s.Compaction.KeepRecentTokens != nil && *s.Compaction.KeepRecentTokens > 0 {
		return *s.Compaction.KeepRecentTokens
	}
	return DefaultCompactionKeepRecentTokens
}

// RetryEnabled reports whether failed assistant turns are retried.
func (s Settings) RetryEnabled() bool {
	if s.Retry != nil && s.Retry.Enabled != nil {
		return *s.Retry.Enabled
	}
	return true
}

// RetryMaxRetries is the number of retry attempts for a failed turn.
func (s Settings) RetryMaxRetries() int {
	if s.Retry != nil && s.Retry.MaxRetries != nil && *s.Retry.MaxRetries >= 0 {
		return *s.Retry.MaxRetries
	}
	return DefaultRetryMaxRetries
}

// RetryBaseDelayMs is the base delay for exponential retry backoff.
func (s Settings) RetryBaseDelayMs() int {
	if s.Retry != nil && s.Retry.BaseDelayMs != nil && *s.Retry.BaseDelayMs >= 0 {
		return *s.Retry.BaseDelayMs
	}
	return DefaultRetryBaseDelayMs
}

// TransportOrDefault returns the configured transport, defaulting to "auto".
func (s Settings) TransportOrDefault() string {
	if s.Transport != "" {
		return s.Transport
	}
	return DefaultTransport
}

// ThinkingLevelForModel returns the reasoning level saved for modelID, if any.
func (s Settings) ThinkingLevelForModel(modelID string) (string, bool) {
	if modelID == "" {
		return "", false
	}
	level, ok := s.Reasoning[modelID]
	return level, ok && level != ""
}

// PatchGlobalSettings merges patch into ~/.maiku/agent/settings.json,
// preserving unknown keys. Creates the file and parent dirs when missing.
func PatchGlobalSettings(agentDir string, patch map[string]any) error {
	if agentDir == "" {
		agentDir = codingagent.GetAgentDir()
	}
	path := GlobalSettingsPath(codingagent.ExpandTildePath(agentDir))
	raw, err := readSettingsFile(path)
	if err != nil {
		return err
	}
	if raw == nil {
		raw = map[string]any{}
	}
	for k, v := range patch {
		if v == nil {
			delete(raw, k)
			continue
		}
		raw[k] = v
	}
	return writeSettingsFile(path, raw)
}

func writeSettingsFile(path string, raw map[string]any) error {
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// SetDefaultModel writes defaultProvider / defaultModel into global settings.
func SetDefaultModel(agentDir, provider, model string) error {
	return PatchGlobalSettings(agentDir, map[string]any{
		"defaultProvider": provider,
		"defaultModel":    model,
	})
}

// SetModelThinkingLevel records the reasoning level for a single model id in
// global settings. Entries for other models are preserved, so switching models
// keeps each model's own reasoning preference. An empty level removes the entry.
func SetModelThinkingLevel(agentDir, modelID, level string) error {
	if agentDir == "" {
		agentDir = codingagent.GetAgentDir()
	}
	path := GlobalSettingsPath(codingagent.ExpandTildePath(agentDir))
	raw, err := readSettingsFile(path)
	if err != nil {
		return err
	}
	if raw == nil {
		raw = map[string]any{}
	}
	reasoning, _ := raw["reasoning"].(map[string]any)
	if reasoning == nil {
		reasoning = map[string]any{}
	}
	if level == "" {
		delete(reasoning, modelID)
	} else {
		reasoning[modelID] = level
	}
	if len(reasoning) == 0 {
		delete(raw, "reasoning")
	} else {
		raw["reasoning"] = reasoning
	}
	return writeSettingsFile(path, raw)
}

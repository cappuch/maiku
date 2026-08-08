package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mikus/maiku/codingagent"
)

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadSettingsMergesProjectOverGlobal(t *testing.T) {
	cwd := t.TempDir()
	agentDir := t.TempDir()

	writeFile(t, GlobalSettingsPath(agentDir), `{
		"defaultProvider": "anthropic",
		"defaultModel": "claude-sonnet-4-5",
		"defaultThinkingLevel": "medium",
		"reasoning": { "model-a": "high" },
		"shellPath": "/bin/zsh",
		"compaction": { "enabled": true, "reserveTokens": 1000, "keepRecentTokens": 2000 },
		"retry": { "maxRetries": 5, "provider": { "timeoutMs": 1234 } }
	}`)
	writeFile(t, ProjectSettingsPath(cwd), `{
		"defaultModel": "claude-opus-4-5",
		"reasoning": { "model-b": "low" },
		"compaction": { "reserveTokens": 4096 },
		"retry": { "provider": { "maxRetries": 9 } }
	}`)

	result := LoadSettings(cwd, agentDir)
	if len(result.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	settings := result.Settings
	if settings.DefaultProvider != "anthropic" {
		t.Errorf("defaultProvider = %q, want inherited %q", settings.DefaultProvider, "anthropic")
	}
	if settings.DefaultModel != "claude-opus-4-5" {
		t.Errorf("defaultModel = %q, want project override", settings.DefaultModel)
	}
	if settings.ShellPath != "/bin/zsh" {
		t.Errorf("shellPath = %q, want inherited", settings.ShellPath)
	}

	// Nested objects merge key-by-key instead of being replaced wholesale.
	if got := settings.CompactionReserveTokens(); got != 4096 {
		t.Errorf("compaction.reserveTokens = %d, want 4096", got)
	}
	if got := settings.CompactionKeepRecentTokens(); got != 2000 {
		t.Errorf("compaction.keepRecentTokens = %d, want inherited 2000", got)
	}
	if got := settings.RetryMaxRetries(); got != 5 {
		t.Errorf("retry.maxRetries = %d, want inherited 5", got)
	}
	if settings.Retry == nil || settings.Retry.Provider == nil {
		t.Fatal("retry.provider missing")
	}
	if got := settings.Retry.Provider.TimeoutMs; got == nil || *got != 1234 {
		t.Errorf("retry.provider.timeoutMs = %v, want inherited 1234", got)
	}
	if got := settings.Retry.Provider.MaxRetries; got == nil || *got != 9 {
		t.Errorf("retry.provider.maxRetries = %v, want project 9", got)
	}

	// Per-model reasoning maps merge key-by-key like other nested objects.
	if level, ok := settings.ThinkingLevelForModel("model-a"); !ok || level != "high" {
		t.Errorf("model-a reasoning = %q, %v; want high, true", level, ok)
	}
	if level, ok := settings.ThinkingLevelForModel("model-b"); !ok || level != "low" {
		t.Errorf("model-b reasoning = %q, %v; want low, true", level, ok)
	}
}

func TestSetModelThinkingLevelPersistsPerModel(t *testing.T) {
	agentDir := t.TempDir()
	if err := SetModelThinkingLevel(agentDir, "model-a", "high"); err != nil {
		t.Fatal(err)
	}
	// Writing a second model must not clobber the first, and re-writing one
	// model must not affect the others.
	if err := SetModelThinkingLevel(agentDir, "model-b", "low"); err != nil {
		t.Fatal(err)
	}
	if err := SetModelThinkingLevel(agentDir, "model-b", "max"); err != nil {
		t.Fatal(err)
	}

	result := LoadSettings(t.TempDir(), agentDir)
	if level, ok := result.Settings.ThinkingLevelForModel("model-a"); !ok || level != "high" {
		t.Errorf("model-a reasoning = %q, %v; want high, true", level, ok)
	}
	if level, ok := result.Settings.ThinkingLevelForModel("model-b"); !ok || level != "max" {
		t.Errorf("model-b reasoning = %q, %v; want max, true", level, ok)
	}
	if _, ok := result.Settings.ThinkingLevelForModel("unknown"); ok {
		t.Error("unknown model should have no saved reasoning level")
	}
	if _, ok := result.Settings.ThinkingLevelForModel(""); ok {
		t.Error("empty model id should have no saved reasoning level")
	}

	// An empty level removes the entry, leaving the others intact.
	if err := SetModelThinkingLevel(agentDir, "model-a", ""); err != nil {
		t.Fatal(err)
	}
	result = LoadSettings(t.TempDir(), agentDir)
	if _, ok := result.Settings.ThinkingLevelForModel("model-a"); ok {
		t.Error("cleared model-a reasoning should be gone")
	}
	if level, ok := result.Settings.ThinkingLevelForModel("model-b"); !ok || level != "max" {
		t.Errorf("model-b reasoning = %q, %v; want max, true", level, ok)
	}
}

func TestLoadSettingsDefaultsWithoutFiles(t *testing.T) {
	result := LoadSettings(t.TempDir(), t.TempDir())
	if len(result.Errors) != 0 {
		t.Fatalf("missing files should not error: %v", result.Errors)
	}

	settings := result.Settings
	if !settings.CompactionEnabled() {
		t.Error("compaction should default to enabled")
	}
	if got := settings.CompactionReserveTokens(); got != DefaultCompactionReserveTokens {
		t.Errorf("reserveTokens = %d, want %d", got, DefaultCompactionReserveTokens)
	}
	if got := settings.CompactionKeepRecentTokens(); got != DefaultCompactionKeepRecentTokens {
		t.Errorf("keepRecentTokens = %d, want %d", got, DefaultCompactionKeepRecentTokens)
	}
	if got := settings.TransportOrDefault(); got != DefaultTransport {
		t.Errorf("transport = %q, want %q", got, DefaultTransport)
	}
}

func TestLoadSettingsReportsMalformedFile(t *testing.T) {
	cwd := t.TempDir()
	agentDir := t.TempDir()
	writeFile(t, GlobalSettingsPath(agentDir), "{ not json")
	writeFile(t, ProjectSettingsPath(cwd), `{"defaultModel": "gpt-5"}`)

	result := LoadSettings(cwd, agentDir)
	if len(result.Errors) != 1 || result.Errors[0].Scope != ScopeGlobal {
		t.Fatalf("want one global error, got %v", result.Errors)
	}
	if result.Settings.DefaultModel != "gpt-5" {
		t.Errorf("a broken global file should not discard project settings, got %q", result.Settings.DefaultModel)
	}
}

func TestLoadSettingsKeepsUnmodelledKeys(t *testing.T) {
	agentDir := t.TempDir()
	writeFile(t, GlobalSettingsPath(agentDir), `{"theme": "dark", "keybindings": {"submit": "ctrl+s"}}`)

	result := LoadSettings(t.TempDir(), agentDir)
	if _, ok := result.Raw["keybindings"]; !ok {
		t.Error("unmodelled keys should survive in Raw")
	}
	if result.Settings.Theme != "dark" {
		t.Errorf("theme = %q, want dark", result.Settings.Theme)
	}
}

func TestProjectSettingsPathUsesConfigDirName(t *testing.T) {
	got := ProjectSettingsPath("/tmp/project")
	want := filepath.Join("/tmp/project", codingagent.CONFIG_DIR_NAME, "settings.json")
	if got != want {
		t.Errorf("ProjectSettingsPath = %q, want %q", got, want)
	}
}

package tui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mikus/maiku/ai"
	"github.com/mikus/maiku/codingagent/core"
	mcp "github.com/mikus/maiku/codingagent/core/mcp"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func isolatedModel(t *testing.T) *model {
	t.Helper()
	t.Setenv("MAIKU_AGENT_DIR", t.TempDir())
	t.Setenv("MAIKU_SESSION_DIR", t.TempDir())
	m := newModel(context.Background(), t.TempDir(), options{})
	m.loading = false
	t.Cleanup(func() {
		if m.session != nil {
			m.session.Dispose()
		}
		m.mcp.Close()
	})
	return m
}

func submitCommand(m *model, text string) tea.Cmd {
	m.input.SetValue(text)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return cmd
}

func TestSlashCommandsWithoutSession(t *testing.T) {
	m := isolatedModel(t)
	submitCommand(m, "/sessions")
	if m.picker != "Sessions" {
		t.Fatal("sessions command did not open picker")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	submitCommand(m, "/models")
	if m.picker != "Models" {
		t.Fatal("models command did not open picker")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	submitCommand(m, "/help")
	if !strings.Contains(m.viewport.View(), "/sessions") || len(m.messages) != 0 {
		t.Fatal("commands must show local help without adding conversation messages")
	}
	submitCommand(m, "/unknown")
	if !strings.Contains(m.notice, "Unknown command") || m.busy {
		t.Fatal("unknown command sent to agent")
	}
	submitCommand(m, "/provider add openai")
	if m.form == nil || m.form.input.Value() != "openai" {
		t.Fatal("provider setup unavailable before model selection")
	}
}

func TestCommandsDuringRunDoNotChangeConfiguration(t *testing.T) {
	m := isolatedModel(t)
	m.busy = true
	submitCommand(m, "/mcp add")
	if m.form != nil || !strings.Contains(m.notice, "Finish or stop") {
		t.Fatal("configuration changed during active run")
	}
	if len(m.messages) != 0 {
		t.Fatal("slash command entered conversation")
	}
}

func TestProviderKeyIsMaskedAndCancelDoesNotSave(t *testing.T) {
	m := isolatedModel(t)
	submitCommand(m, "/provider add openai")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.form.fields[m.form.index].key != "key" {
		t.Fatal("expected key field")
	}
	m.form.input.SetValue("test-secret-never-render")
	if strings.Contains(m.View(), "test-secret-never-render") {
		t.Fatal("secret rendered")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.form != nil || len(m.messages) != 0 {
		t.Fatal("cancel did not discard setup")
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("MAIKU_AGENT_DIR"), "auth.json")); !os.IsNotExist(err) {
		t.Fatal("cancel wrote credentials")
	}
}

func TestProviderConfigPreservesOtherProvidersAndSecrets(t *testing.T) {
	dir := t.TempDir()
	store := core.NewAuthStorage(filepath.Join(dir, "auth.json"))
	if err := store.Write("anthropic", core.Credential{Type: core.CredentialAPIKey, Key: "existing"}); err != nil {
		t.Fatal(err)
	}
	values := map[string]string{"id": "test-route", "url": "http://localhost:1234/v1", "models": "one, two", "key": "new-secret"}
	if err := saveProviderConfig(dir, store, values); err != nil {
		t.Fatal(err)
	}
	configured := core.LoadCustomProviders(dir)
	if len(configured) != 1 || len(configured[0].Models) != 2 {
		t.Fatal("custom route not saved")
	}
	if store.APIKey("test-route") != "new-secret" || store.APIKey("anthropic") != "existing" {
		t.Fatal("credentials not preserved")
	}
	settings, err := os.ReadFile(core.GlobalSettingsPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(settings), "new-secret") {
		t.Fatal("secret leaked into provider settings")
	}
	if err := saveProviderConfig(dir, store, map[string]string{"id": "anthropic", "key": ""}); err != nil {
		t.Fatal(err)
	}
	if store.APIKey("anthropic") != "existing" {
		t.Fatal("blank key deleted credential")
	}
}

func TestSetupValidation(t *testing.T) {
	for _, tc := range []struct{ key, value string }{{"url", "file:///tmp/server"}, {"url", "https://user:password@example.com"}, {"transport", "bogus"}, {"args", `["ok",42]`}, {"headers", `{"Authorization":42}`}, {"env", `["wrong"]`}, {"command", ""}} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			f := newForm("MCP", setupField{key: tc.key})
			f.input.SetValue(tc.value)
			if f.accept(t.TempDir(), t.TempDir()) == nil {
				t.Fatal("invalid field accepted")
			}
		})
	}
	f := newForm("MCP", setupField{key: "args"})
	f.input.SetValue(`["C:\\Program Files\\server", "path with spaces"]`)
	if err := f.accept(t.TempDir(), t.TempDir()); err != nil {
		t.Fatal(err)
	}
}

func TestMCPSetupConnectsAndUpdatesSessionTools(t *testing.T) {
	m := isolatedModel(t)
	m.selected = ai.Model{ID: "test-model", Provider: "test-provider"}
	if err := m.useSession(nil); err != nil {
		t.Fatal(err)
	}
	sessionID := m.session.SessionManager().Header().ID
	server := sdk.NewServer(&sdk.Implementation{Name: "test", Version: "1"}, nil)
	sdk.AddTool(server, &sdk.Tool{Name: "ping", Description: "test"}, func(context.Context, *sdk.CallToolRequest, struct{}) (*sdk.CallToolResult, struct{}, error) {
		return &sdk.CallToolResult{}, struct{}{}, nil
	})
	handler := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return server }, nil)
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()
	defer m.mcp.Close()
	form := &setupForm{kind: "MCP", values: map[string]string{"name": "test-server", "transport": "http", "url": httpServer.URL}}
	result := saveSetup(m.ctx, m.cwd, os.Getenv("MAIKU_AGENT_DIR"), m.mcp, form)
	if result.err != nil {
		t.Fatal(result.err)
	}
	if m.mcp.Snapshot().Connected != 1 {
		t.Fatalf("server did not connect: %s", result.notice)
	}
	m.Update(result)
	if m.session.SessionManager().Header().ID != sessionID {
		t.Fatal("configuration reset conversation")
	}
	found := false
	for _, tool := range m.session.State().Tools {
		if strings.Contains(tool.Name, "ping") {
			found = true
		}
	}
	if !found {
		t.Fatal("MCP tool not installed into active session")
	}
	loaded := mcp.Load(m.cwd, os.Getenv("MAIKU_AGENT_DIR"))
	if loaded.Servers["test-server"].URL != httpServer.URL {
		t.Fatal("server config not persisted")
	}
}

func TestModelsDirectSelectionAndSessionResume(t *testing.T) {
	m := isolatedModel(t)
	err := core.UpsertCustomProvider(os.Getenv("MAIKU_AGENT_DIR"), core.CustomProvider{ID: "test-selector", BaseURL: "http://localhost:1234/v1", Models: []string{"one", "two"}})
	if err != nil {
		t.Fatal(err)
	}
	submitCommand(m, "/models test-selector/one")
	if m.session == nil || m.selected.ID != "one" {
		t.Fatalf("model not selected: %s", m.notice)
	}
	id := m.session.SessionManager().Header().ID
	submitCommand(m, "/models test-selector/two")
	if m.selected.ID != "two" || m.session.SessionManager().Header().ID != id {
		t.Fatal("model selection reset session")
	}
	submitCommand(m, "/sessions new")
	if m.session.SessionManager().Header().ID == id {
		t.Fatal("new session not created")
	}
	submitCommand(m, "/sessions "+id)
	if m.session.SessionManager().Header().ID != id {
		t.Fatalf("session not resumed: %s", m.notice)
	}
}

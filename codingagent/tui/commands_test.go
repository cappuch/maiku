package tui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cappuch/maiku/ai"
	"github.com/cappuch/maiku/codingagent"
	"github.com/cappuch/maiku/codingagent/core"
	"github.com/cappuch/maiku/codingagent/core/compaction"
	mcp "github.com/cappuch/maiku/codingagent/core/mcp"
	tea "github.com/charmbracelet/bubbletea"
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

func TestCompactCommandRequiresASession(t *testing.T) {
	m := isolatedModel(t)
	if cmd := submitCommand(m, "/compact extra"); cmd != nil || !strings.Contains(m.notice, "Usage: /compact") || m.busy {
		t.Fatal("compact accepted arguments")
	}
	submitCommand(m, "/compact")
	if !strings.Contains(m.notice, "No active session") || m.busy || len(m.messages) != 0 {
		t.Fatal("compact ran without a session")
	}
}

func TestCompactCommandRewritesTheSession(t *testing.T) {
	m := isolatedModel(t)
	manager := core.NewSessionManager(t.TempDir(), t.TempDir(), true)
	messages := []ai.Message{
		{Role: "user", UserContent: strings.Repeat("old ", 2000), Timestamp: 1},
		{Role: "assistant", AssistantContent: []ai.AssistantContentBlock{ai.TextBlock("old reply")}, Timestamp: 2},
		{Role: "user", UserContent: strings.Repeat("recent ", 2000), Timestamp: 3},
	}
	for _, message := range messages {
		if err := manager.AppendMessage(message); err != nil {
			t.Fatal(err)
		}
	}
	m.session = core.NewAgentSession(core.AgentSessionOptions{
		Model:      ai.Model{ID: "test", Provider: "test", ContextWindow: 100_000, MaxTokens: 4096},
		Sessions:   manager,
		Compaction: compaction.Settings{Enabled: true, ReserveTokens: 1000, KeepRecentTokens: 300},
		StreamFn: func(model ai.Model, _ ai.Context, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			stream := ai.NewAssistantMessageEventStream()
			message := ai.AssistantMessage{
				Role:       "assistant",
				Content:    []ai.AssistantContentBlock{ai.TextBlock("SUMMARY TEXT")},
				Model:      model.ID,
				Usage:      ai.EmptyUsage(),
				StopReason: ai.StopStop,
			}
			stream.Push(ai.AssistantMessageEvent{Type: "done", Reason: ai.StopStop, Message: &message})
			return stream
		},
	})
	m.messages = manager.Messages()

	cmd := submitCommand(m, "/compact")
	if cmd == nil || !m.busy || m.status != "Compacting…" {
		t.Fatal("compact did not start")
	}
	if cmd() != nil {
		t.Fatal("compact command returned a message directly")
	}
	select {
	case msg := <-m.events:
		m.Update(msg)
	default:
		t.Fatal("compact did not finish")
	}
	if m.busy || !strings.Contains(m.notice, "Compacted") {
		t.Fatalf("compact result = busy:%v notice:%q", m.busy, m.notice)
	}
	if len(m.messages) >= len(messages) || len(m.messages) == 0 {
		t.Fatalf("transcript length = %d, want a shorter summary", len(m.messages))
	}
	reloaded, err := core.LoadSessionManager(manager.File())
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Messages()) != len(m.messages) {
		t.Fatalf("saved transcript = %d messages, view has %d", len(reloaded.Messages()), len(m.messages))
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

func TestEditingCustomProviderPrefillsAndKeepsKey(t *testing.T) {
	dir := t.TempDir()
	store := core.NewAuthStorage(filepath.Join(dir, "auth.json"))
	if err := store.Write("acme", core.Credential{Type: core.CredentialAPIKey, Key: "kept-secret"}); err != nil {
		t.Fatal(err)
	}
	if err := core.UpsertCustomProvider(dir, core.CustomProvider{
		ID: "acme", Name: "Acme", BaseURL: "https://old.example/v1", API: "openai-completions", Models: []string{"acme-large"},
	}); err != nil {
		t.Fatal(err)
	}
	f := newForm("provider", setupField{key: "id"})
	f.input.SetValue("acme")
	if err := f.accept(t.TempDir(), dir); err != nil {
		t.Fatal(err)
	}
	if !f.editing || f.values["url"] != "https://old.example/v1" || f.values["display"] != "Acme" || f.values["models"] != "acme-large" {
		t.Fatalf("edit form was not prefilled: editing=%v values=%v", f.editing, f.values)
	}
	if err := saveProviderConfig(dir, store, map[string]string{
		"id": "acme", "display": "Acme Cloud", "url": "https://new.example/v1", "api": "openai-responses", "models": "acme-large", "key": "",
	}); err != nil {
		t.Fatal(err)
	}
	configured := core.LoadCustomProviders(dir)
	if len(configured) != 1 || configured[0].BaseURL != "https://new.example/v1" || configured[0].Name != "Acme Cloud" || configured[0].API != "openai-responses" {
		t.Fatalf("provider = %+v", configured)
	}
	if store.APIKey("acme") != "kept-secret" {
		t.Fatalf("key = %q", store.APIKey("acme"))
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

func TestUseSessionDoesNotCreateEmptyFile(t *testing.T) {
	m := isolatedModel(t)
	m.selected = ai.Model{ID: "test-model", Provider: "test-provider"}
	if err := m.useSession(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(m.session.SessionManager().File()); !os.IsNotExist(err) {
		t.Fatalf("empty session file exists: %v", err)
	}
}

func TestAbandonedEmptySessionIsDeleted(t *testing.T) {
	m := isolatedModel(t)
	m.selected = ai.Model{ID: "test-model", Provider: "test-provider"}
	if err := m.useSession(nil); err != nil {
		t.Fatal(err)
	}
	previous := m.session.SessionManager()
	if err := previous.EnsurePersisted(); err != nil {
		t.Fatal(err)
	}
	if err := m.useSession(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(previous.File()); !os.IsNotExist(err) {
		t.Fatalf("abandoned empty session still present: %v", err)
	}
	if _, err := os.Stat(m.session.SessionManager().File()); !os.IsNotExist(err) {
		t.Fatalf("replacement session was written before a chat: %v", err)
	}
}

func TestSessionPickerOmitsEmptySessions(t *testing.T) {
	m := isolatedModel(t)
	dir := codingagent.GetDefaultSessionDir(m.cwd)
	empty := core.NewSessionManager(m.cwd, dir, true)
	if err := empty.EnsurePersisted(); err != nil {
		t.Fatal(err)
	}
	full := core.NewSessionManager(m.cwd, dir, true)
	if err := full.AppendMessage(ai.Message{
		Role:        "user",
		UserContent: []ai.TextContent{{Type: "text", Text: "hello"}},
	}); err != nil {
		t.Fatal(err)
	}
	m.openPicker(false)
	if len(m.choices) != 1 || m.choices[0].path != full.File() {
		t.Fatalf("choices = %+v", m.choices)
	}
	if _, err := os.Stat(empty.File()); !os.IsNotExist(err) {
		t.Fatalf("empty session still listed on disk: %v", err)
	}
}

func TestOpenExistingRecallsSessionModel(t *testing.T) {
	m := isolatedModel(t)
	writeAcmeModels(t)
	m.selected = ai.Model{ID: "acme-small", Provider: "acme"}
	dir := codingagent.GetDefaultSessionDir(m.cwd)
	session := core.NewSessionManager(m.cwd, dir, true)
	if err := session.SetModel("acme", "acme-large"); err != nil {
		t.Fatal(err)
	}
	if err := session.AppendMessage(ai.Message{
		Role:        "user",
		UserContent: []ai.TextContent{{Type: "text", Text: "hello"}},
	}); err != nil {
		t.Fatal(err)
	}
	loaded, err := core.LoadSessionManager(session.File())
	if err != nil {
		t.Fatal(err)
	}
	if err := m.openExisting(loaded); err != nil {
		t.Fatal(err)
	}
	if m.selected.Provider != "acme" || m.selected.ID != "acme-large" {
		t.Fatalf("model = %s/%s", m.selected.Provider, m.selected.ID)
	}
}

func TestLaunchFlagOverridesSessionModel(t *testing.T) {
	m := isolatedModel(t)
	writeAcmeModels(t)
	m.opts.model = "acme-small"
	m.selected = ai.Model{ID: "acme-small", Provider: "acme"}
	dir := codingagent.GetDefaultSessionDir(m.cwd)
	session := core.NewSessionManager(m.cwd, dir, true)
	if err := session.SetModel("acme", "acme-large"); err != nil {
		t.Fatal(err)
	}
	if err := session.AppendMessage(ai.Message{
		Role:        "user",
		UserContent: []ai.TextContent{{Type: "text", Text: "hello"}},
	}); err != nil {
		t.Fatal(err)
	}
	loaded, err := core.LoadSessionManager(session.File())
	if err != nil {
		t.Fatal(err)
	}
	if err := m.bindLaunchSession(loaded); err != nil {
		t.Fatal(err)
	}
	if m.selected.ID != "acme-small" {
		t.Fatalf("flag model lost: %s", m.selected.ID)
	}
	provider, modelID := loaded.RememberedModel()
	if provider != "acme" || modelID != "acme-small" {
		t.Fatalf("session now remembers %s/%s", provider, modelID)
	}
}

func writeAcmeModels(t *testing.T) {
	t.Helper()
	body := []byte(`{"customProviders":[{"id":"acme","name":"Acme","baseUrl":"https://example.com/v1","models":["acme-large","acme-small"]}]}`)
	if err := os.WriteFile(filepath.Join(os.Getenv("MAIKU_AGENT_DIR"), "settings.json"), body, 0o644); err != nil {
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
	if err := m.session.SessionManager().AppendMessage(ai.Message{
		Role:        "user",
		UserContent: []ai.TextContent{{Type: "text", Text: "hello"}},
	}); err != nil {
		t.Fatal(err)
	}
	submitCommand(m, "/sessions new")
	if m.session.SessionManager().Header().ID == id {
		t.Fatal("new session not created")
	}
	m.selected = ai.Model{ID: "one", Provider: "test-selector"}
	submitCommand(m, "/sessions "+id)
	if m.session.SessionManager().Header().ID != id {
		t.Fatalf("session not resumed: %s", m.notice)
	}
	if m.selected.Provider != "test-selector" || m.selected.ID != "two" {
		t.Fatalf("resumed model = %s/%s", m.selected.Provider, m.selected.ID)
	}
}

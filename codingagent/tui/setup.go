package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mikus/maiku/ai/providers"
	"github.com/mikus/maiku/codingagent"
	"github.com/mikus/maiku/codingagent/core"
	mcp "github.com/mikus/maiku/codingagent/core/mcp"
)

type setupField struct {
	key, label, hint string
	secret           bool
}
type setupForm struct {
	kind   string
	fields []setupField
	values map[string]string
	index  int
	input  textinput.Model
	err    string
}

func newForm(kind string, first setupField) *setupForm {
	f := &setupForm{kind: kind, fields: []setupField{first}, values: map[string]string{}}
	f.resetInput()
	return f
}
func (f *setupForm) resetInput() {
	f.input = textinput.New()
	f.input.CharLimit = 0
	f.input.Focus()
	field := f.fields[f.index]
	f.input.Placeholder = field.hint
	if field.secret {
		f.input.EchoMode = textinput.EchoPassword
		f.input.EchoCharacter = '•'
	}
	f.input.SetValue(f.values[field.key])
	f.err = ""
}
func (m *model) startProviderForm(id string) tea.Cmd {
	m.form = newForm("provider", setupField{key: "id", label: "Provider ID", hint: "openai, anthropic, or a new custom ID"})
	m.form.input.SetValue(id)
	return textinput.Blink
}
func (m *model) startMCPForm() tea.Cmd {
	m.form = newForm("MCP", setupField{key: "name", label: "Server name", hint: "a unique name"})
	return textinput.Blink
}

func validEndpoint(value string) bool {
	u, err := url.Parse(value)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil
}

var providerID = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,63}$`)

func (f *setupForm) accept(cwd, agentDir string) error {
	field := f.fields[f.index]
	value := strings.TrimSpace(f.input.Value())
	switch field.key {
	case "id":
		value = strings.ToLower(value)
		if !providerID.MatchString(value) {
			return fmt.Errorf("use 2–64 lowercase letters, digits, underscores, or hyphens")
		}
		if _, builtin := providers.Find(value); builtin {
			f.fields = append(f.fields, setupField{key: "key", label: "API key", hint: "blank keeps the existing key or environment setting", secret: true})
		} else {
			for _, p := range core.LoadCustomProviders(agentDir) {
				if p.ID == value {
					return fmt.Errorf("that custom provider already exists; choose a new ID")
				}
			}
			f.fields = append(f.fields, setupField{key: "url", label: "OpenAI-compatible base URL", hint: "https://example.com/v1"}, setupField{key: "models", label: "Model IDs (optional)", hint: "comma-separated fallback IDs; blank discovers /models"}, setupField{key: "key", label: "API key", hint: "blank keeps an environment key; keyless servers accept a placeholder", secret: true})
		}
	case "name":
		if value == "" {
			return fmt.Errorf("server name is required")
		}
		loaded := mcp.Load(cwd, agentDir)
		if len(loaded.Errors) > 0 {
			return fmt.Errorf("fix the existing MCP configuration before adding a server")
		}
		if _, exists := loaded.Servers[value]; exists {
			return fmt.Errorf("that server name already exists; choose a new name")
		}
		f.fields = append(f.fields, setupField{key: "transport", label: "Transport", hint: "stdio, http, or sse (blank: stdio)"})
	case "transport":
		value = strings.ToLower(value)
		if value == "" {
			value = "stdio"
		}
		switch value {
		case "stdio":
			f.fields = append(f.fields, setupField{key: "command", label: "Executable", hint: "npx or an absolute executable path; arguments go next"}, setupField{key: "args", label: "Arguments as a JSON array (optional)", hint: `["-y", "@example/mcp-server", "path with spaces"]`}, setupField{key: "env", label: "Environment as a JSON object (optional, masked)", hint: `{"API_KEY":"value"}`, secret: true})
		case "http", "sse":
			f.fields = append(f.fields, setupField{key: "url", label: "Server URL", hint: "https://example.com/mcp"}, setupField{key: "headers", label: "Headers as a JSON object (optional, masked)", hint: `{"Authorization":"Bearer token"}`, secret: true})
		default:
			return fmt.Errorf("transport must be stdio, http, or sse")
		}
	case "url":
		if !validEndpoint(value) {
			return fmt.Errorf("enter an http:// or https:// URL with a host and no embedded credentials")
		}
	case "command":
		if value == "" {
			return fmt.Errorf("an executable is required")
		}
	case "args":
		var args []string
		if value != "" && (json.Unmarshal([]byte(value), &args) != nil || !strings.HasPrefix(value, "[")) {
			return fmt.Errorf("enter a JSON array of strings, or leave blank")
		}
	case "env", "headers":
		var values map[string]string
		if value != "" && (json.Unmarshal([]byte(value), &values) != nil || !strings.HasPrefix(value, "{")) {
			return fmt.Errorf("enter a JSON object with string values, or leave blank")
		}
	}
	f.values[field.key] = value
	return nil
}

func (m *model) updateForm(msg tea.Msg) tea.Cmd {
	f := m.form
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc":
			m.form = nil
			m.status = "Setup cancelled"
			return nil
		case "enter":
			if err := f.accept(m.cwd, codingagent.GetAgentDir()); err != nil {
				f.err = err.Error()
				return nil
			}
			f.index++
			if f.index < len(f.fields) {
				f.resetInput()
				return textinput.Blink
			}
			m.form = nil
			m.configBusy = true
			m.status = "Saving and connecting…"
			ctx, cwd, manager := m.ctx, m.cwd, m.mcp
			return func() tea.Msg { return saveSetup(ctx, cwd, codingagent.GetAgentDir(), manager, f) }
		}
	}
	var cmd tea.Cmd
	f.input, cmd = f.input.Update(msg)
	return cmd
}

func saveSetup(ctx context.Context, cwd, agentDir string, manager *mcp.Manager, f *setupForm) configResult {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	v := f.values
	if f.kind == "provider" {
		id := v["id"]
		if err := saveProviderConfig(agentDir, core.DefaultAuthStorage(), v); err != nil {
			return configResult{err: err}
		}
		notice := "Provider saved. Choose a model with /models."
		if err := core.RefreshProviderModels(ctx, id); err != nil {
			notice = "Provider saved, but model discovery failed. Check the endpoint/key and use /models refresh. Configured fallback models remain available."
		}
		return configResult{notice: notice, openModels: true, rebuild: true}
	}
	cfg := mcp.ServerConfig{Type: v["transport"], Command: v["command"], URL: v["url"]}
	// These values were validated before leaving their fields.
	_ = json.Unmarshal([]byte(v["args"]), &cfg.Args)
	_ = json.Unmarshal([]byte(v["env"]), &cfg.Env)
	_ = json.Unmarshal([]byte(v["headers"]), &cfg.Headers)
	if err := mcp.UpsertGlobal(agentDir, v["name"], cfg); err != nil {
		return configResult{err: err}
	}
	notice := "MCP server saved and connected. Its tools are available now."
	if err := manager.Sync(ctx, cwd, agentDir); err != nil {
		notice = "MCP configuration saved; one or more connections failed. Use /mcp to check status and /mcp reload to retry."
	}
	return configResult{notice: notice, rebuild: true}
}

func saveProviderConfig(agentDir string, store *core.AuthStorage, v map[string]string) error {
	id := v["id"]
	if _, builtin := providers.Find(id); !builtin {
		var models []string
		for _, id := range strings.Split(v["models"], ",") {
			if id = strings.TrimSpace(id); id != "" {
				models = append(models, id)
			}
		}
		if err := core.UpsertCustomProvider(agentDir, core.CustomProvider{ID: id, Name: id, BaseURL: v["url"], Models: models}); err != nil {
			return err
		}
	}
	if v["key"] != "" {
		if err := store.Write(id, core.Credential{Type: core.CredentialAPIKey, Key: v["key"]}); err != nil {
			return fmt.Errorf("could not save the API key: %w", err)
		}
	}
	return nil
}

func (f *setupForm) View(width, height int) string {
	input := f.input
	input.Width = max(1, width-4)
	field := f.fields[f.index]
	text := accent.Render("Add "+f.kind) + "\n\n" + field.label + "\n" + muted.Render(field.hint) + "\n\n" + input.View() + "\n\n" + f.err + "\n\nEnter continues / saves the final field · Esc cancels\nSaved globally; existing configuration is preserved."
	return lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Render(text)
}

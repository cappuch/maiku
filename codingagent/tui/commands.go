package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mikus/maiku/codingagent"
	"github.com/mikus/maiku/codingagent/core"
)

const commandHelp = `Commands

/sessions                 Choose a saved session
/sessions new             Start a new session (also /new)
/sessions <path or ID>     Resume a session in this folder
/models                   Choose a model
/models <provider/model>  Switch models directly
/models refresh           Refresh configured provider catalogs
/providers                List providers and credential status
/provider add [ID]        Add a provider or update a built-in API key
/mcp                      List MCP servers and connection status
/mcp add                  Add a stdio, HTTP, or SSE server
/mcp reload               Reload connections and available tools
/help                     Show this help
/stop                     Stop the current run
/quit                     Exit

Setup is saved globally and shared with the desktop app.
API keys are entered in a masked field, never added to the conversation.
Use // at the start to send a literal slash-prefixed prompt.`

var slashCommands = []string{"/sessions", "/models", "/providers", "/provider add", "/mcp add", "/mcp reload", "/mcp", "/new", "/help", "/stop", "/quit"}

func commandMatches(value string) []string {
	if !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") {
		return nil
	}
	var matches []string
	for _, command := range slashCommands {
		if strings.HasPrefix(command, value) {
			matches = append(matches, command)
		}
	}
	return matches
}

type configResult struct {
	notice              string
	err                 error
	rebuild, openModels bool
}

func (m *model) showNotice(text string) {
	m.notice = text
	m.refresh(false)
	m.viewport.GotoTop()
}

func (m *model) command(text string) tea.Cmd {
	name, rest, _ := strings.Cut(text, " ")
	rest = strings.TrimSpace(rest)
	switch name {
	case "/help":
		m.showNotice(commandHelp)
		return nil
	case "/stop":
		if m.busy && m.session != nil {
			m.session.Abort()
			m.status = "Stopping…"
		}
		return nil
	case "/quit", "/exit":
		if m.session != nil {
			m.session.Abort()
		}
		return tea.Quit
	}
	if m.busy || m.configBusy {
		m.showNotice("Finish or stop the current operation before changing configuration or sessions.")
		return nil
	}
	switch name {
	case "/sessions", "/session":
		switch rest {
		case "":
			m.openPicker(false)
		case "new":
			m.newSession()
		default:
			path, err := core.ResolveSessionPath([]string{codingagent.GetDefaultSessionDir(m.cwd)}, rest)
			if err == nil {
				var manager *core.SessionManager
				manager, err = core.LoadSessionManager(path)
				if err == nil {
					err = m.useSession(manager)
				}
			}
			if err != nil {
				m.showNotice(err.Error())
			} else {
				m.notice = ""
				m.status = "Session opened"
				m.refresh(true)
			}
		}
	case "/new":
		m.newSession()
	case "/models", "/model":
		if rest == "refresh" {
			return m.refreshModels()
		}
		if rest == "" {
			m.openPicker(true)
			return nil
		}
		selected, err := core.ResolveModel(core.ResolveModelOptions{Model: rest})
		if err != nil {
			m.showNotice(err.Error() + "\nUse /models refresh to fetch configured catalogs.")
			return nil
		}
		previous := m.selected
		m.selected = selected
		if err := m.rebuildSession(); err != nil {
			m.selected = previous
			m.showNotice(err.Error())
		} else {
			m.notice = ""
			m.status = "Model: " + selected.Provider + "/" + selected.ID
			m.refresh(false)
		}
	case "/providers", "/provider":
		if rest == "add" || strings.HasPrefix(rest, "add ") {
			return m.startProviderForm(strings.TrimSpace(strings.TrimPrefix(rest, "add")))
		}
		if rest != "" {
			m.showNotice("Usage: /providers or /provider add [ID]")
			return nil
		}
		var rows []string
		for _, provider := range core.AllProviders() {
			status := "no key"
			if core.HasAPIKey(provider.ID) {
				status = "configured"
			}
			rows = append(rows, fmt.Sprintf("%s · %s · %d models", provider.ID, status, len(provider.Models)))
		}
		sort.Strings(rows)
		m.showNotice("Providers\n\n" + strings.Join(rows, "\n") + "\n\n/provider add [ID] to configure a provider.")
	case "/mcp", "/mcps":
		switch rest {
		case "add":
			return m.startMCPForm()
		case "reload":
			return m.reloadMCP()
		case "":
			status := m.mcp.Snapshot()
			rows := []string{"MCP servers"}
			for _, s := range status.Servers {
				state := "idle"
				if s.Disabled {
					state = "disabled"
				} else if s.Connected {
					state = "connected"
				} else if s.Error != "" {
					state = "connection failed"
				}
				rows = append(rows, fmt.Sprintf("%s · %s · %s · %d tools", s.Name, s.Kind, state, s.ToolCount))
			}
			if len(status.Servers) == 0 {
				rows = append(rows, "No servers connected yet.")
			}
			rows = append(rows, "", "/mcp add to configure a server; /mcp reload to reconnect.")
			m.showNotice(strings.Join(rows, "\n"))
		default:
			m.showNotice("Usage: /mcp, /mcp add, or /mcp reload")
		}
	default:
		m.showNotice("Unknown command. Type /help for commands, or // to send a literal slash-prefixed prompt.")
	}
	return nil
}

func (m *model) newSession() {
	if err := m.useSession(nil); err != nil {
		m.showNotice(err.Error())
		return
	}
	m.notice = ""
	m.status = "New session"
	m.refresh(true)
}

func (m *model) rebuildSession() error {
	if m.selected.ID == "" {
		return nil
	}
	var manager *core.SessionManager
	if m.session != nil {
		manager = m.session.SessionManager()
	}
	return m.useSession(manager)
}

func (m *model) refreshModels() tea.Cmd {
	m.configBusy = true
	m.status = "Refreshing models…"
	ctx := m.ctx
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		var failed []string
		for _, p := range core.AllProviders() {
			if !core.HasAPIKey(p.ID) && len(p.Models) == 0 {
				continue
			}
			if err := core.RefreshProviderModels(ctx, p.ID); err != nil {
				failed = append(failed, p.ID)
			}
		}
		notice := "Model catalogs refreshed."
		if len(failed) > 0 {
			notice += " Could not refresh: " + strings.Join(failed, ", ") + ". Existing models are still available."
		}
		return configResult{notice: notice, openModels: true}
	}
}

func (m *model) reloadMCP() tea.Cmd {
	m.configBusy = true
	m.status = "Connecting MCP servers…"
	ctx, cwd, manager := m.ctx, m.cwd, m.mcp
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		err := manager.Sync(ctx, cwd, codingagent.GetAgentDir())
		notice := "MCP connections reloaded."
		if err != nil {
			notice = "Some MCP connections failed. Use /mcp to inspect server status."
		}
		return configResult{notice: notice, rebuild: true}
	}
}

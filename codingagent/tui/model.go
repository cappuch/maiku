package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cappuch/maiku/agent"
	"github.com/cappuch/maiku/ai"
	"github.com/cappuch/maiku/codingagent"
	"github.com/cappuch/maiku/codingagent/core"
	mcp "github.com/cappuch/maiku/codingagent/core/mcp"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var accent = lipgloss.NewStyle().Foreground(lipgloss.Color("#f0b75a"))
var muted = lipgloss.NewStyle().Foreground(lipgloss.Color("#9999a5"))

type doneMsg struct {
	err  error
	text string
	gen  uint64
}
type choice struct {
	label, path string
	model       ai.Model
}
type model struct {
	ctx            context.Context
	cwd            string
	opts           options
	input          textarea.Model
	viewport       viewport.Model
	width, height  int
	session        *core.AgentSession
	subagents      *core.SubagentRunner
	selected       ai.Model
	mcp            *mcp.Manager
	events         chan tea.Msg
	messages       []ai.Message
	live           *ai.Message
	loading, busy  bool
	status, picker string
	choices        []choice
	cursor         int
	notice         string
	form           *setupForm
	configBusy     bool
	login          *loginAttempt
	queue          []string
	promptGen      uint64
}

const (
	composerPlaceholder = "Ask maiku, or type /help for commands…"
	queuePlaceholder    = "Queue a follow-up…"
)

func newModel(ctx context.Context, cwd string, opts options) *model {
	input := textarea.New()
	input.Placeholder = composerPlaceholder
	input.ShowLineNumbers = false
	input.CharLimit = 0
	input.SetHeight(3)
	input.Focus()
	input.KeyMap.InsertNewline.SetKeys("alt+enter", "ctrl+j")
	return &model{ctx: ctx, cwd: cwd, opts: opts, input: input, viewport: viewport.New(80, 15), width: 80, height: 24, mcp: mcp.NewManager(), events: make(chan tea.Msg, 128), loading: true, status: "Connecting…"}
}
func (m *model) Init() tea.Cmd {
	worker := *m
	return tea.Batch(func() tea.Msg {
		result := worker.initialize().(readyMsg)
		if worker.ctx.Err() != nil {
			if worker.session != nil {
				worker.session.Dispose()
			}
			worker.mcp.Close()
			return nil
		}
		result.runtime = &worker
		return result
	}, textarea.Blink)
}
func (m *model) waitEvent() tea.Msg {
	select {
	case e := <-m.events:
		return e
	case <-m.ctx.Done():
		return nil
	}
}
func (m *model) resize() {
	m.layout()
	m.refresh(false)
}

func (m *model) queueChrome() int {
	if len(m.queue) == 0 {
		return 0
	}
	return 1 + min(len(m.queue), 4)
}

func (m *model) layout() {
	m.input.SetWidth(max(1, m.width-4))
	m.viewport.Width = max(1, m.width-4)
	m.viewport.Height = max(1, m.height-10-m.queueChrome())
}

func (m *model) refresh(bottom bool) {
	m.layout()
	var b strings.Builder
	for _, msg := range m.messages {
		b.WriteString(renderMessage(msg))
		b.WriteString("\n\n")
	}
	if m.live != nil {
		b.WriteString(renderMessage(*m.live))
	}
	if b.Len() == 0 {
		b.WriteString("What are we building?\n\nDescribe a change, ask about this repo, or resume a session.\n\n/sessions  Saved conversations\n/models    Choose a model\n/help      All commands and setup")
	}
	if !m.loading && m.session == nil {
		b.WriteString("\n\nConfigure a provider with /provider add, then choose a model with /models.")
	}
	if m.notice != "" {
		b.Reset()
		b.WriteString(m.notice)
		if m.login == nil {
			b.WriteString("\n\nEsc returns to the conversation.")
		}
	}
	m.viewport.SetContent(ansi.Hardwrap(b.String(), max(1, m.viewport.Width), true))
	if bottom {
		m.viewport.GotoBottom()
	}
}
func renderMessage(msg ai.Message) string {
	var b strings.Builder
	switch msg.Role {
	case "user":
		b.WriteString(accent.Render("you") + "\n")
		if user, ok := msg.AsUser(); ok {
			if text, ok := user.Content.(string); ok {
				b.WriteString(text)
			} else {
				encoded, _ := json.Marshal(user.Content)
				var blocks []struct {
					Type string
					Text string
				}
				if json.Unmarshal(encoded, &blocks) == nil {
					for _, block := range blocks {
						if block.Type == "text" {
							b.WriteString(block.Text)
						} else if block.Type == "image" {
							b.WriteString("[image]")
						}
					}
				}
			}
		}
	case "assistant":
		b.WriteString(accent.Render("maiku") + "\n")
		for _, c := range msg.AssistantContent {
			switch c.Type {
			case "text":
				b.WriteString(c.Text)
			case "thinking":
				b.WriteString(muted.Render(c.Thinking) + "\n")
			case "toolCall":
				b.WriteString(muted.Render("↳ "+c.Name) + "\n")
			}
		}
		if msg.ErrorMessage != "" {
			b.WriteString("\n" + msg.ErrorMessage)
		}
	case "toolResult":
		b.WriteString(muted.Render("↳ " + msg.ToolName))
		if msg.IsError {
			b.WriteString(" · failed")
		} else {
			b.WriteString(" · done")
		}
		for _, c := range msg.ToolContent {
			if c.Text != "" {
				text := c.Text
				if len([]rune(text)) > 2000 {
					text = string([]rune(text)[:2000]) + "…"
				}
				b.WriteString("\n" + text)
			}
		}
	}
	return b.String()
}

func (m *model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case loginStarted:
		return m, m.handleLoginStarted(msg)
	case loginFinished:
		if m.login != msg.attempt {
			return m, nil
		}
		m.login.cancel()
		m.login = nil
		return m.Update(msg.result)
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resize()
		return m, nil
	case readyMsg:
		if msg.runtime != nil {
			m.session, m.selected, m.messages = msg.runtime.session, msg.runtime.selected, msg.runtime.messages
			m.subagents = msg.runtime.subagents
		}
		m.loading = false
		m.status = "Ready"
		if msg.err != nil {
			m.status = msg.err.Error()
		}
		m.refresh(true)
		return m, m.waitEvent
	case configResult:
		m.configBusy = false
		m.status = msg.notice
		if msg.err != nil {
			m.status = msg.err.Error()
			m.showNotice(m.status)
			return m, nil
		}
		if msg.rebuild {
			if err := m.rebuildSession(); err != nil {
				m.status = err.Error()
				m.showNotice(m.status)
				return m, nil
			}
		}
		m.showNotice(msg.notice)
		if msg.openModels {
			m.openPicker(true)
		}
		return m, nil
	case doneMsg:
		m.live = nil
		if msg.gen != m.promptGen {
			m.busy = false
			if m.status == "Stopping…" {
				m.status = "Ready"
				m.input.Placeholder = composerPlaceholder
			}
			m.refresh(false)
			return m, m.waitEvent
		}
		if msg.err != nil {
			m.busy = false
			m.status = msg.err.Error()
			m.input.Placeholder = composerPlaceholder
			if msg.text != "" {
				m.queue = append([]string{msg.text}, m.queue...)
			}
			m.refresh(false)
			return m, m.waitEvent
		}
		m.busy = false
		if cmd := m.flushQueue(true); cmd != nil {
			return m, cmd
		}
		m.status = "Ready"
		m.input.Placeholder = composerPlaceholder
		m.refresh(false)
		return m, m.waitEvent
	case agent.AgentEvent:
		bottom := m.viewport.AtBottom()
		switch msg.Type {
		case agent.EventMessageUpdate:
			copy := msg.Message
			m.live = &copy
		case agent.EventMessageEnd:
			m.messages = append(m.messages, msg.Message)
			m.live = nil
		case agent.EventToolExecutionStart:
			m.status = "Running " + msg.ToolName
		case agent.EventToolExecutionEnd:
			m.status = "Working…"
		}
		m.refresh(bottom)
		return m, m.waitEvent
	case tea.KeyMsg:
		if m.login != nil {
			switch msg.String() {
			case "esc", "ctrl+c":
				m.cancelLogin()
			case "pgup", "pgdown", "ctrl+home", "ctrl+end":
				var cmd tea.Cmd
				m.viewport, cmd = m.viewport.Update(msg)
				return m, cmd
			}
			return m, nil
		}
		if msg.String() == "ctrl+c" {
			if m.busy {
				m.stop()
				m.refresh(false)
				return m, nil
			}
			return m, tea.Quit
		}
		if m.loading {
			return m, nil
		}
		if m.form != nil {
			return m, m.updateForm(msg)
		}
		if m.configBusy {
			return m, nil
		}
		if m.picker != "" {
			return m, m.updatePicker(msg)
		}
		switch msg.String() {
		case "tab":
			if matches := commandMatches(m.input.Value()); len(matches) > 0 {
				m.input.SetValue(matches[0] + " ")
				m.input.CursorEnd()
				return m, nil
			}
		case "esc":
			if m.busy {
				m.stop()
				m.notice = ""
				m.refresh(false)
				return m, nil
			}
			if len(m.queue) > 0 {
				m.queue = nil
				m.status = "Queue cleared"
				m.notice = ""
				m.refresh(false)
				return m, nil
			}
			m.notice = ""
			m.refresh(false)
			return m, nil
		case "ctrl+n":
			if !m.busy && m.session != nil {
				m.newSession()
			}
			return m, nil
		case "ctrl+s", "ctrl+o":
			if !m.busy {
				m.openPicker(msg.String() == "ctrl+o")
			}
			return m, nil
		case "pgup", "pgdown", "ctrl+home", "ctrl+end":
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		case "up", "down":
			// A single-line composer has nothing to navigate. Over SSH the
			// scroll wheel often arrives as these keys and would otherwise
			// scroll the input's inner viewport into blank padding.
			if m.input.LineCount() <= 1 {
				var cmd tea.Cmd
				m.viewport, cmd = m.viewport.Update(msg)
				return m, cmd
			}
		case "enter":
			text := strings.TrimSpace(m.input.Value())
			if strings.HasPrefix(text, "/") && !strings.HasPrefix(text, "//") {
				m.input.Reset()
				return m, m.command(text)
			}
			if strings.HasPrefix(text, "//") {
				text = strings.TrimPrefix(text, "/")
			}
			if text == "" {
				if !m.busy && len(m.queue) > 0 {
					m.notice = ""
					return m, m.flushQueue(false)
				}
				return m, nil
			}
			if m.session == nil {
				return m, nil
			}
			if m.busy {
				m.queue = append(m.queue, text)
				m.input.Reset()
				m.notice = ""
				m.refresh(false)
				return m, nil
			}
			m.input.Reset()
			m.notice = ""
			m.busy = true
			m.status = "Working…"
			m.input.Placeholder = queuePlaceholder
			m.refresh(true)
			return m, m.runPrompt(text)
		}
	}
	if m.form != nil {
		return m, m.updateForm(message)
	}
	if key, ok := message.(tea.KeyMsg); ok && keyIsTerminalScroll(key) {
		return m, nil
	}
	var cmd tea.Cmd
	if _, ok := message.(tea.MouseMsg); ok {
		// Scroll the transcript only. The composer must not receive wheel
		// events; over SSH those sequences otherwise land in the input.
		m.viewport, cmd = m.viewport.Update(message)
		return m, cmd
	}
	m.input, cmd = m.input.Update(message)
	m.sanitizeInput()
	return m, cmd
}

func (m *model) stop() {
	if m.session != nil {
		m.session.Abort()
	}
	m.queue = nil
	m.promptGen++
	m.status = "Stopping…"
	m.input.Placeholder = composerPlaceholder
}

func (m *model) runPrompt(text string) tea.Cmd {
	session := m.session
	gen := m.promptGen
	return func() tea.Msg {
		var err error
		if session != nil {
			err = session.Prompt(m.ctx, text)
		}
		select {
		case m.events <- doneMsg{err: err, text: text, gen: gen}:
		case <-m.ctx.Done():
		}
		return nil
	}
}

func (m *model) flushQueue(wait bool) tea.Cmd {
	if len(m.queue) == 0 || m.session == nil {
		return nil
	}
	next := m.queue[0]
	m.queue = m.queue[1:]
	m.busy = true
	m.status = "Working…"
	m.input.Placeholder = queuePlaceholder
	m.refresh(true)
	cmd := m.runPrompt(next)
	if wait {
		// doneMsg was delivered by waitEvent, so that listener has returned.
		return tea.Batch(m.waitEvent, cmd)
	}
	return cmd
}

func (m *model) openPicker(models bool) {
	m.cursor = 0
	m.choices = nil
	if models {
		m.picker = "Models"
		for _, model := range core.AllModels() {
			m.choices = append(m.choices, choice{label: model.Provider + " / " + model.ID, model: model})
		}
	} else {
		m.picker = "Sessions"
		core.PruneEmptySessions(codingagent.GetSessionsDir(), nil)
		entries, _ := os.ReadDir(codingagent.GetDefaultSessionDir(m.cwd))
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
				continue
			}
			path := filepath.Join(codingagent.GetDefaultSessionDir(m.cwd), entry.Name())
			manager, err := core.LoadSessionManager(path)
			if err != nil || manager.Header().Cwd != m.cwd || len(manager.Messages()) == 0 {
				continue
			}
			label := "New conversation"
			for _, message := range manager.Messages() {
				if message.Role == "user" {
					label = strings.Join(strings.Fields(ansi.Strip(renderMessage(message))), " ")
					label = strings.TrimPrefix(label, "you ")
					break
				}
			}
			m.choices = append(m.choices, choice{label: ansi.Truncate(label, 60, "…") + " · " + manager.Header().ID, path: path})
		}
		sort.Slice(m.choices, func(i, j int) bool { return m.choices[i].path > m.choices[j].path })
	}
}
func (m *model) updatePicker(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.picker = ""
	case "up", "k":
		m.cursor = max(0, m.cursor-1)
	case "down", "j":
		m.cursor = min(max(0, len(m.choices)-1), m.cursor+1)
	case "enter":
		if len(m.choices) == 0 {
			return nil
		}
		c := m.choices[m.cursor]
		var manager *core.SessionManager
		var err error
		if m.picker == "Models" {
			m.selected = c.model
			if m.session != nil {
				manager = m.session.SessionManager()
			}
		} else {
			manager, err = core.LoadSessionManager(c.path)
		}
		if err == nil {
			if m.picker == "Models" {
				err = m.useSession(manager)
			} else {
				err = m.openExisting(manager)
			}
		}
		if err != nil {
			m.status = err.Error()
		} else {
			m.status = "Ready"
			m.notice = ""
		}
		m.picker = ""
		m.refresh(true)
	}
	return nil
}
func (m *model) View() string {
	header := accent.Render("maiku") + muted.Render(" / "+filepath.Base(m.cwd)+"   ") + m.selected.ID
	body := m.viewport.View()
	if m.picker != "" {
		var b strings.Builder
		b.WriteString(accent.Render(m.picker) + "\n\n")
		if len(m.choices) == 0 {
			b.WriteString("No entries available.")
		}
		start := max(0, m.cursor-max(1, m.viewport.Height-4)/2)
		for i := start; i < len(m.choices) && i < start+max(1, m.viewport.Height-3); i++ {
			prefix := "  "
			if i == m.cursor {
				prefix = "› "
			}
			b.WriteString(ansi.Truncate(prefix+m.choices[i].label, m.viewport.Width, "…") + "\n")
		}
		body = lipgloss.NewStyle().Height(m.viewport.Height).Render(b.String())
	}
	if m.form != nil {
		body = m.form.View(m.viewport.Width, m.viewport.Height)
	}
	status := m.status
	if matches := commandMatches(m.input.Value()); len(matches) > 0 && m.form == nil && m.picker == "" {
		status = "Tab completes · " + strings.Join(matches, "  ")
	}
	if m.session != nil {
		var tokens int
		for _, msg := range m.messages {
			if msg.Usage != nil {
				tokens += msg.Usage.TotalTokens
			}
		}
		snapshot := m.mcp.Snapshot()
		status += fmt.Sprintf("  · %d tokens · MCP %d/%d", tokens, snapshot.Connected, snapshot.Configured)
	}
	if len(m.queue) > 0 {
		status += fmt.Sprintf("  · %d queued", len(m.queue))
	}
	composer := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#303039")).Render(m.composerView())
	below := body + "\n"
	if queued := m.queueView(); queued != "" {
		below += queued + "\n"
	}
	below += composer
	hint := "/help commands · Enter send · Enter queues while working · Alt+Enter newline · PgUp/Dn scroll · Esc stop · ^C quit"
	return lipgloss.NewStyle().Padding(0, 1).Render(ansi.Truncate(header, max(1, m.width-2), "…") + "\n" + muted.Render(strings.Repeat("─", max(1, m.width-2))) + "\n" + below + "\n" + muted.Render(ansi.Truncate(status, max(1, m.width-2), "…")) + "\n" + muted.Render(ansi.Truncate(hint, max(1, m.width-2), "…")))
}

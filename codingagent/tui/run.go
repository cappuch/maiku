package tui

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cappuch/maiku/agent"
	"github.com/cappuch/maiku/ai"
	"github.com/cappuch/maiku/codingagent"
	"github.com/cappuch/maiku/codingagent/core"
	"github.com/cappuch/maiku/codingagent/core/compaction"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"
)

type options struct {
	provider, model, session string
	resume                   bool
}

func Run(args []string, input *os.File, output, errors io.Writer) int {
	var opts options
	flags := flag.NewFlagSet("maiku", flag.ContinueOnError)
	flags.SetOutput(errors)
	flags.StringVar(&opts.provider, "provider", "", "Provider to use")
	flags.StringVar(&opts.model, "model", "", "Model to use")
	flags.StringVar(&opts.session, "session", "", "Resume a session path or ID")
	flags.BoolVar(&opts.resume, "continue", false, "Resume the latest session in this folder")
	flags.Usage = func() {
		fmt.Fprintln(errors, "Usage: maiku [--provider NAME] [--model ID] [--continue | --session PATH] [prompt]\n\nInteractive terminal workspace. Enter sends; Alt+Enter adds a line.\nCommands: /sessions, /models, /provider add, /codex-login, /miru-login, /mcp add, /help\nCtrl+N new session · Ctrl+S sessions · Ctrl+O models · Esc stop · Ctrl+C quit")
		flags.PrintDefaults()
		fmt.Fprintln(errors, "\nUpdates: maiku update [--check]\nSet MAIKU_AUTO_UPDATE=0 to disable automatic updates.")
	}
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if !term.IsTerminal(input.Fd()) {
		fmt.Fprintln(errors, "maiku: interactive mode requires a terminal; use --help for options")
		return 2
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(errors, err)
		return 1
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := newModel(ctx, cwd, opts)
	m.input.SetValue(strings.Join(flags.Args(), " "))
	defer func() {
		cancel()
		if m.subagents != nil {
			m.subagents.AbortAll()
		}
		if m.session != nil {
			m.session.Abort()
			m.session.Dispose()
		}
		m.mcp.Close()
	}()
	_, err = tea.NewProgram(m, tea.WithInput(input), tea.WithOutput(output), tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	if err != nil {
		fmt.Fprintln(errors, err)
		return 1
	}
	return 0
}

type readyMsg struct {
	err     error
	runtime *model
}

func (m *model) initialize() tea.Msg {
	core.InstallAuthStorage(core.DefaultAuthStorage())
	settings := core.LoadSettings(m.cwd, codingagent.GetAgentDir()).Settings
	provider := m.opts.provider
	if provider == "" {
		provider = settings.DefaultProvider
	}
	if provider == "" {
		for _, p := range core.AllProviders() {
			if core.HasAPIKey(p.ID) {
				provider = p.ID
				break
			}
		}
	}
	ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
	defer cancel()
	if provider != "" {
		_ = core.RefreshProviderModels(ctx, provider)
	}
	selected := m.opts.model
	if selected == "" {
		selected = settings.DefaultModel
	}
	resolved, err := core.ResolveModel(core.ResolveModelOptions{Provider: provider, Model: selected})
	_ = m.mcp.Sync(ctx, m.cwd, codingagent.GetAgentDir())
	if err != nil {
		return readyMsg{err: err}
	}
	m.selected = resolved
	dir := codingagent.GetDefaultSessionDir(m.cwd)
	path := m.opts.session
	if path != "" {
		path, err = core.ResolveSessionPath([]string{dir}, path)
	} else if m.opts.resume {
		path = core.FindMostRecentSession(dir, m.cwd)
	}
	if err != nil {
		return readyMsg{err: err}
	}
	var manager *core.SessionManager
	if path != "" {
		manager, err = core.LoadSessionManager(path)
		if err != nil {
			return readyMsg{err: err}
		}
	}
	return readyMsg{err: m.useSession(manager)}
}

func (m *model) useSession(manager *core.SessionManager) error {
	if m.selected.ID == "" {
		return fmt.Errorf("select a configured model before opening a session")
	}
	if manager == nil {
		manager = core.NewSessionManager(m.cwd, codingagent.GetDefaultSessionDir(m.cwd), true)
	}
	if err := manager.EnsurePersisted(); err != nil {
		return err
	}
	if filepath.Clean(manager.Header().Cwd) != filepath.Clean(m.cwd) {
		return fmt.Errorf("session belongs to %s; launch maiku from that folder to resume it", manager.Header().Cwd)
	}
	if m.session != nil {
		m.session.Dispose()
	}
	if m.subagents != nil {
		m.subagents.AbortAll()
	}
	settings := core.LoadSettings(m.cwd, codingagent.GetAgentDir()).Settings
	retry := ai.RetryPolicy{Enabled: settings.RetryEnabled(), MaxRetries: settings.RetryMaxRetries(), BaseDelayMs: settings.RetryBaseDelayMs(), MaxDelayMs: settings.RetryMaxDelayMs()}
	m.subagents = nil
	if settings.SubagentEnabled() {
		m.subagents = core.NewSubagentRunner(core.SubagentToolOptions{Cwd: m.cwd, AgentDir: codingagent.GetAgentDir(), Model: m.selected, ThinkingLevel: agent.ThinkingLevel(settings.DefaultThinkingLevel), Retry: retry, ShellPath: settings.ShellPath, ShellCommandPrefix: settings.ShellCommandPrefix})
	}
	skills := core.LoadSkills(core.LoadSkillsOptions{Cwd: m.cwd, AgentDir: codingagent.GetAgentDir(), SkillPaths: settings.Skills, IncludeDefaults: true})
	available := append(core.SelectRootToolsWithOptions(m.cwd, nil, nil, false, m.subagents, core.ToolOptions{ShellPath: settings.ShellPath, ShellCommandPrefix: settings.ShellCommandPrefix}), m.mcp.Tools()...)
	names := make([]string, 0, len(available))
	for _, tool := range available {
		names = append(names, tool.Name)
	}
	m.session = core.NewAgentSession(core.AgentSessionOptions{
		Model: m.selected, ThinkingLevel: agent.ThinkingLevel(settings.DefaultThinkingLevel), Sessions: manager, Tools: available,
		Retry:        retry,
		SystemPrompt: core.BuildSystemPrompt(core.BuildSystemPromptOptions{Cwd: m.cwd, SelectedTools: names, ToolSnippets: m.mcp.ToolSnippets(), ContextFiles: core.LoadProjectContextFiles(m.cwd, codingagent.GetAgentDir()), Skills: skills.Skills}),
		Compaction:   compaction.Settings{Enabled: settings.CompactionEnabled(), ReserveTokens: settings.CompactionReserveTokens(), KeepRecentTokens: settings.CompactionKeepRecentTokens()},
	})
	m.messages = manager.Messages()
	m.session.Subscribe(func(event agent.AgentEvent) {
		select {
		case m.events <- event:
		case <-m.ctx.Done():
		}
	})
	return nil
}

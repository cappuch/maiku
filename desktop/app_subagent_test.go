package main

import (
	"strings"
	"testing"

	"github.com/mikus/maiku/agent"
	"github.com/mikus/maiku/codingagent"
	"github.com/mikus/maiku/codingagent/core"
)

func includesTool(tools []agent.AgentTool, name string) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

func TestRootAgentConfigHonorsSubagentSetting(t *testing.T) {
	cwd := t.TempDir()
	agentDir := t.TempDir()
	runner := core.NewSubagentRunner(core.SubagentToolOptions{Cwd: cwd, AgentDir: agentDir})

	tools, prompt := rootAgentConfig(cwd, agentDir, runner, true)
	if !includesTool(tools, core.SubagentToolName) {
		t.Fatal("enabled root config is missing the subagent tool")
	}
	if !strings.Contains(prompt, "- subagent:") || !strings.Contains(prompt, "Use subagents") {
		t.Fatal("enabled root prompt is missing subagent guidance")
	}

	tools, prompt = rootAgentConfig(cwd, agentDir, runner, false)
	if includesTool(tools, core.SubagentToolName) {
		t.Fatal("disabled root config still includes the subagent tool")
	}
	if strings.Contains(prompt, "- subagent:") || strings.Contains(prompt, "Use subagents") {
		t.Fatal("disabled root prompt still includes subagent guidance")
	}
}

func TestSetSubagentEnabledUpdatesLiveSessions(t *testing.T) {
	cwd := t.TempDir()
	agentDir := t.TempDir()
	t.Setenv(codingagent.ENV_AGENT_DIR, agentDir)

	runner := core.NewSubagentRunner(core.SubagentToolOptions{Cwd: cwd, AgentDir: agentDir})
	tools, prompt := rootAgentConfig(cwd, agentDir, runner, true)
	session := core.NewAgentSession(core.AgentSessionOptions{
		SystemPrompt: prompt,
		Tools:        tools,
	})
	defer session.Dispose()

	app := &App{
		cwd: cwd,
		live: map[string]*liveSession{
			"live": {id: "live", session: session, subagents: runner},
		},
		activeID: "live",
	}

	if err := app.SetSubagentEnabled(false); err != nil {
		t.Fatal(err)
	}
	state := session.State()
	if includesTool(state.Tools, core.SubagentToolName) {
		t.Fatal("live session retained subagent after disabling it")
	}
	if strings.Contains(state.SystemPrompt, "- subagent:") {
		t.Fatal("live session retained subagent prompt guidance after disabling it")
	}
	if core.LoadSettings(cwd, agentDir).Settings.SubagentEnabled() {
		t.Fatal("disabled setting was not persisted")
	}

	if err := app.SetSubagentEnabled(true); err != nil {
		t.Fatal(err)
	}
	state = session.State()
	if !includesTool(state.Tools, core.SubagentToolName) {
		t.Fatal("live session did not restore subagent after enabling it")
	}
	if !strings.Contains(state.SystemPrompt, "- subagent:") {
		t.Fatal("live session did not restore subagent prompt guidance")
	}
	if !core.LoadSettings(cwd, agentDir).Settings.SubagentEnabled() {
		t.Fatal("enabled setting was not persisted")
	}
}
